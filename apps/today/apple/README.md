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
| `UI/Pages/`      | the pages: priorities, all projects, a project, a person, a meeting, a task, the plan                   |
| `Tests/`         | the gallery: every component, every scene and every page drawn on the Mac, and the hand-made scenes     |

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
| The save card       | `SuggestedCardView`, `WrapCardView`, `PlanPageView`, with `SaveRowView` |
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

Two hooks, in a debug build only:

- `SUBSTRATE_TODAY_DEMO=<scene>` (or the launch argument `-SUBSTRATE_TODAY_DEMO <scene>`) draws one of
  `TodayFixtures.all` by name (`today-working`, or `working`) with no server and no session. Its commands
  go nowhere; lines still open in place and pages open from the fixture.
- `SUBSTRATE_TODAY_SESSION="<server> <repository> <token>"` opens a real server with a token already held,
  without the approval page. The session is written under a Keychain service and a folder of its own
  (`dev.reamde.substrate.today.debug`, `SubstrateToday-Debug`), so it shares nothing with a signed-in
  build. A fourth word, `app`, keeps it as an app session with `full`; without it the session reads as a
  password one.

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
