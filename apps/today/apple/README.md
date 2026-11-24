# Substrate Today's screens

`SubstrateTodayUI` draws what `TodayCore` presents and sends its commands, and decides nothing: every word on
a screen is already in the presentation ([the seam](../docs/seam.md)). The look is the design guide of
proposal 098: one pane, the order of the day.

| Where            | What                                                                                                    |
| ---------------- | ------------------------------------------------------------------------------------------------------- |
| `Sources/App/`   | the app target: its scenes and the updater                                                              |
| `UI/TodayShell`  | the kit's `AppShell` over `TodayApp`, asking for `full`, and the two debug hooks                        |
| `UI/Look/`       | `TodayLook`: the guide's colours, sizes and type, on the kit's tokens where the kit has one             |
| `UI/Components/` | the guide's seventeen components                                                                        |
| `UI/Views/`      | the day (`DayScreen`, `PaneView`), what the views keep to themselves (`PaneViewState`), the root        |
| `UI/Pages/`      | the pages: priorities, all projects, a project, a person, a meeting, a task, the plan, the suggested tasks |
| `Widgets/`       | the widget extension (`SubstrateTodayWidgets`, iPhone and iPad): the three widgets and the timeline it reads |
| `Widgets/Views/` | the widgets' views and their look, compiled into the extension and into the tests                       |
| `Widgets/Shared/`| the Keychain item the widgets' day is kept in, compiled into the extension and into `SubstrateTodayUI`  |
| `Tests/`         | the gallery: every component, every scene, every page and every widget drawn on the Mac, and the hand-made scenes |

## The components

Each is a view of its own, named for what the guide calls it. They live in the app's framework for now; the
option list, the flag stripe and a pill with a glyph are the ones proposed for the kit, and move there when a
second app draws them.

| Guide               | View                                                                    |
| ------------------- | ----------------------------------------------------------------------- |
| Project tile        | `TileView`, and `ProjectChipView` where it is named                     |
| Person badge        | `BadgeView`, and `PersonChipView`                                       |
| Kind glyphs         | `GlyphView`, `IconView`, `RowChevron`                                   |
| Flag stripe         | `StripeView`, `FlagSwatch`, `.stripe(_:)`                               |
| Pill                | `PillView`                                                              |
| Buttons             | `TodayButtonStyle`, `IconButton`, `PaneButton`                          |
| Option list         | `OptionListView`, `DoorView`, `FlagOptionsView`                         |
| Rows                | `RowView`, `MeetingRowView`, `TaskLineView`, `RowList`                  |
| The current item    | `RowView` where the row leads, with its body from `DayCards`            |
| The strip           | `StripView`                                                             |
| What is happening   | `HappeningView`                                                         |
| The urgent card     | `UrgentCardView`, on `.card(urgent: true)`                              |
| The save card       | `SuggestedGroupView` (a project's suggestions; `SuggestedItemView` for one, with `SuggestedOtherField` and `SuggestedToldView`), `WrapCardView`, `PlanPageView` (with `PlanLeftView`), with `SaveRowView` |
| Add or ask          | `ComposerBar`                                                           |
| Moss's box          | `MossBoxView`, `AnswerCardView`                                         |
| A page              | `PageScaffold`, `PageTitle`, `PageView`                                 |
| Ground colours      | `GroundView`                                                            |

The rules the guide gives hold in the views, not in each screen:

- A choice is always an `OptionListView`; every row stays, and a tap only sends the row's command.
- Anything that acts is a `Button` in a `TodayButtonStyle` or an `IconButton`, with a 44 point target.
- `RowChevron` is the only arrow: right for a page, down for what opens in place, up while it is open.
- A flag is drawn by `StripeView` alone, and its word is given to VoiceOver as the row's value.
- Colour is `TodayLook`'s and nothing else names one. The mock-up is light only; the dark values are
  `TodayLook`'s second column, chosen so words stay at 4.5:1 and the three flags stay apart in lightness.
- Text is a Dynamic Type style through `TodayLook.font`. On a Mac, which has no text size of its own, the
  pane is drawn at the sizes an iPhone draws by default, so it is one design on every screen.

On a screen 900 points wide or more the day keeps its column and the page a route names opens beside it; with
no page open the list of projects stands there.

## Driving a debug build

Three hooks, in a debug build only:

- `SUBSTRATE_TODAY_DEMO=<scene>` (or the launch argument `-SUBSTRATE_TODAY_DEMO <scene>`) draws one of
  `TodayFixtures.all` by name (`today-working`, or `working`) with no server and no session. Its commands
  go nowhere; lines still open in place and pages open from the fixture.
- `SUBSTRATE_TODAY_SESSION="<server> <repository> <token>"` opens a real server with a token already held,
  without the approval page. The session is written under a Keychain service and a folder of its own
  (`dev.reamde.substrate.today.debug`, `SubstrateToday-Debug`), so it shares nothing with a signed-in
  build. A fourth word, `app`, keeps it as an app session with `full`; without it the session reads as a
  password one.

- `SUBSTRATE_TODAY_ZONE=<IANA zone>` reads the day in that time zone. With `dev/seed.py --timezone` naming the
  same zone, the invented day can be tried at another hour than this machine's: `Etc/GMT+12` is the morning when
  it is evening in London.

On a simulator the environment reaches the app as `SIMCTL_CHILD_SUBSTRATE_TODAY_DEMO`, and so on.

## The gallery

```bash
mise run apple:test:today   # TodayKit's tests, then every picture must draw
mise run apps:gallery       # the same pictures, written under build/gallery/apple/today
```

`Tests/GalleryTests.swift` draws, at an iPhone's size in light and in dark: each of the guide's components
with its states (`guide-<id>`), each scene of `TodayFixtures.all` and each page it names (`scene-…`), the
whole day drawn long (`long-…`), the wide layout at an iPad's and a Mac's size (`wide-…`), and the states one
invented day does not reach, from values made by hand in `Tests/HandScenes.swift` (`hand-…`).

A picture is drawn offscreen on the Mac, so glass is a translucent fill there, and a text size above the
default changes only the layout that depends on it: a Mac has no text size to grow.

## The widgets

`Widgets/` is the extension: `SubstrateTodayWidgets` (the bundle and the three `StaticConfiguration`s, medium
and large), and `GlanceProvider`, which reads the Keychain item the app wrote and hands WidgetKit the days in
it. It works nothing out: with no item, its one entry says to open the app.

`Widgets/Shared/` is that item, `GlanceKeychain`: one generic password (service
`dev.reamde.substrate.today.glance`, account `day`, readable after the first unlock, never synchronised) whose
value is the timeline's JSON compressed. It is compiled into the extension, which reads it, and into
`SubstrateTodayUI`, whose `TodayGlanceWriter` writes it. It names no access group: the app's entitlements and
the extension's each list the app's own identifier as their one Keychain group
(`$(AppIdentifierPrefix)dev.reamde.substrate.today`), so that is where a write goes and what a read searches,
as Chat's session reaches its notification extension. A Keychain group is no capability, so nothing is
registered for it. `GlanceKeychainTests` holds what names the item, the group in the three entitlements
files, and the value there and back.

`Widgets/Views/` draws a `GlanceDay` and decides nothing: `GlanceWidgetView` is one widget at one instant, over
`NowGlanceView`, `TasksGlanceView` and `MeetingsGlanceView`. The extension links no part of the app, so
`GlanceLook` repeats the few colours, sizes and glyphs a widget draws with, and `WidgetLookTests` holds each to
`TodayLook`'s and to the app's own glyphs.

- Every widget is rows and nothing else: no name over them, and no row taller than one line. `GlanceRow`
  draws one (`GlanceRowContent`, made from a `GlanceItem` or a `GlanceMeeting`): the stripe, the glyph in a
  box as tall as the words (so every row is one height whatever its glyph), a meeting's start and end, the
  title, and what ends the row. `ViewThatFits` drops what the title has no room for, in order: the quiet
  words at the end, then a meeting's end, and only then is the title cut. In the Meetings widget the times
  keep the room of the widest, so the titles stand on one edge.
- The type is the app's, two steps smaller (`GlanceLook.Role`): 13 points for a title, 12 for a time, 11 for
  what is said quietly, and a row is 20 points, so a medium widget holds six rows and a large one sixteen.
  A weight is set on the font (`Font.system(style, design:).weight(…)`): a text style named with its weight
  drew at the regular one on a device.
- A flag is its stripe alone, as in the pane, hung in the widget's margin as the stripe of the pane's leading
  row is, so rows with a flag and rows without one keep their glyphs on one edge. Where the system draws a
  widget in one tint, the glyphs and the stripes are what it tints (`widgetAccentable`), and High is the most
  solid of the three.
- The count to a meeting (`GlanceCount`) is the system's own count of whole minutes over the time left
  (`Date.ComponentsFormatStyle`, "12 min") after the core's word "in", so it moves by itself and reads
  "in 12 min". The system drops a minute that has started and the app counts it, so the stretch it counts
  runs 59.9 seconds past the start; `WidgetLookTests` holds what it says to `GlanceWords.count(from:to:)`,
  the words a picture draws. At the start itself the timeline's next day takes the count away.
- A list draws as many rows as fit (`GlanceList`), so a larger text size draws fewer and cuts none, and one
  quiet line under them where a line is left over: how the day stands ("2 of 6 tasks done"). Now and next
  always reaches the next fixed point: where it would fall below the last row, the rows before it give way
  and one quiet row says how many ("and 4 more").
- The background is the card colour, given to the system as the widget's container background.

The gallery draws them at an iPhone's sizes, 338 by 158 and 338 by 354, in light and in dark:
`widget-<now|tasks|meetings>-<medium|large>-<moment>` for each of `TodayFixtures.glances` (waking, the morning,
the afternoon in a meeting with two to choose between, a repository with nothing to draw, signed out), and
`widget-sheet-<moment>`, the three side by side in both sizes. `widget-sheet-afternoon-large-text` is drawn a
third larger, and `widget-sheet-afternoon-tinted` as the widgets draw themselves for one tint, without the
system's tint over them. A picture's count to a meeting stands at the day's own instant, in the same words
the running one says; on a device it runs.
