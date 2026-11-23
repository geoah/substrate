#if os(macOS)
import AppKit
import SubstrateUI
import SwiftUI
import Testing
import TodayCore
import TodayGlance

@testable import SubstrateTodayUI

/// A widget as the home screen shows it, for a picture: the shared view
/// inside the margins the system gives a widget, on the card colour, at an
/// iPhone's size for the family. The count to a meeting stands at the
/// day's own instant, so the picture is the same whenever it is drawn.
struct WidgetFrame: View {
    /// The points an iPhone gives a medium and a large widget.
    static let medium = CGSize(width: 338, height: 158)
    static let large = CGSize(width: 338, height: 354)
    /// The margin the system puts around a widget's content.
    static let margin: CGFloat = 16

    var kind: GlanceKind
    var day: GlanceDay
    var size: GlanceSize

    var points: CGSize { size == .large ? Self.large : Self.medium }

    var body: some View {
        GlanceWidgetView(kind: kind, day: day, size: size)
            .padding(Self.margin)
            .frame(width: points.width, height: points.height)
            .background(GlanceLook.card.color)
            .clipShape(RoundedRectangle(cornerRadius: 24, style: .continuous))
            .environment(\.glancePaused, true)
    }
}

/// The three widgets of one moment side by side, each in both sizes, on a
/// ground a card stands out from: what a reader compares them by.
struct WidgetSheet: View {
    static let ground = GlanceLook.Colour(light: 0xDFE2E8, dark: 0x0B0C0F)

    static let gap: CGFloat = 20
    static let points = CGSize(
        width: 3 * WidgetFrame.medium.width + 4 * gap, height: WidgetFrame.medium.height + WidgetFrame.large.height + 3 * gap)

    var day: GlanceDay

    var body: some View {
        HStack(alignment: .top, spacing: Self.gap) {
            ForEach(GlanceKind.allCases, id: \.self) { kind in
                VStack(spacing: Self.gap) {
                    WidgetFrame(kind: kind, day: day, size: .medium)
                    WidgetFrame(kind: kind, day: day, size: .large)
                }
            }
        }
        .padding(Self.gap)
        .frame(width: Self.points.width, height: Self.points.height)
        .background(Self.ground.color)
    }
}

extension GalleryTests {
    static func name(_ kind: GlanceKind) -> String {
        switch kind {
        case .now: "now"
        case .tasks: "tasks"
        case .meetings: "meetings"
        }
    }

    /// Every widget in both sizes, in light and in dark, at each moment
    /// the core's fixtures name (`widget-<type>-<size>-<moment>`), and the
    /// three side by side (`widget-sheet-<moment>`).
    @Test func everyWidgetDraws() async throws {
        for (moment, day) in TodayFixtures.glances {
            for kind in GlanceKind.allCases {
                for (size, word) in [(GlanceSize.medium, "medium"), (.large, "large")] {
                    let frame = WidgetFrame(kind: kind, day: day, size: size)
                    try await both("widget-\(Self.name(kind))-\(word)-\(moment)", size: frame.points) { frame }
                }
            }
            try await both("widget-sheet-\(moment)", size: WidgetSheet.points) { WidgetSheet(day: day) }
        }
    }

    /// What the widget gallery and a widget not yet read show: the
    /// invented day the extension carries.
    @Test func theWidgetsPlaceholderDayDraws() async throws {
        let day = GlanceDay.sample(at: TodayFixtures.at("11:30"))
        try await both("widget-sheet-sample", size: WidgetSheet.points) { WidgetSheet(day: day) }
    }

    /// A larger text size, a tint in place of colour, and the long names
    /// of a crowded day: fewer lines are drawn, and none is cut short of
    /// its line.
    @Test func widgetsHoldAtALargerTextSizeAndInOneTint() async throws {
        guard let afternoon = TodayFixtures.glances.first(where: { $0.name == "afternoon" })?.day else { return }
        try await both("widget-sheet-afternoon-large-text", size: WidgetSheet.points) {
            WidgetSheet(day: afternoon).environment(\.glanceScale, 1.35)
        }
        try await both("widget-sheet-afternoon-tinted", size: WidgetSheet.points) {
            WidgetSheet(day: afternoon).environment(\.glanceFlat, true)
        }
    }
}

/// The widgets' look is the app's: the extension links no part of the app,
/// so what it repeats is held to the app's own values here.
@MainActor
@Suite struct WidgetLookTests {
    @Test func everyColourAWidgetDrawsWithIsTheApps() {
        let pairs: [(String, GlanceLook.Colour, TodayLook.Colour)] = [
            ("ink", GlanceLook.ink, TodayLook.ink), ("inkSecond", GlanceLook.inkSecond, TodayLook.inkSecond), ("card", GlanceLook.card, TodayLook.card),
            ("urgent", GlanceLook.urgent, TodayLook.urgent), ("cautionWords", GlanceLook.cautionWords, TodayLook.cautionWords),
            ("cautionFill", GlanceLook.cautionFill, TodayLook.cautionFill), ("flagHigh", GlanceLook.flagHigh, TodayLook.flagHigh),
            ("flagMedium", GlanceLook.flagMedium, TodayLook.flagMedium), ("flagLow", GlanceLook.flagLow, TodayLook.flagLow),
        ]
        for (name, widget, app) in pairs {
            #expect(widget.light == app.light && widget.dark == app.dark, "\(name)")
        }
        #expect(GlanceLook.stripeWidth == TodayLook.stripeWidth)
        for flag in GlanceFlag.allCases {
            #expect(Flag(rawValue: flag.rawValue)?.word == flag.word, "\(flag)")
        }
    }

    @Test func everyGlyphAWidgetDrawsIsTheApps() {
        for glyph in GlanceGlyph.allCases {
            #expect(Glyph(rawValue: glyph.rawValue)?.systemName == glyph.symbol, "\(glyph)")
        }
        #expect(GlanceLook.points(.headline) == TodayLook.points(.headline) && GlanceLook.points(.caption) == TodayLook.points(.caption))
        #expect(GlanceLook.points(.subheadline) == TodayLook.points(.subheadline) && GlanceLook.points(.footnote) == TodayLook.points(.footnote))
    }

    /// On the home screen the system counts the minutes to a meeting
    /// itself. What it says, under the word before it, is what the core
    /// says for a picture: minutes, a started one counted, and never
    /// minutes and seconds.
    @Test func theRunningCountSaysTheCoresWords() {
        let start = TodayFixtures.at("14:40")
        for seconds in [3600.0, 3599, 3541, 3540, 3300, 900, 899, 870, 841, 840, 61, 60, 59, 31, 30, 2, 1] {
            let now = start.addingTimeInterval(-seconds)
            let running = GlanceCount.minutes.format(now ..< start.addingTimeInterval(GlanceCount.started))
            #expect("\(GlanceWords.until) \(running)" == GlanceWords.count(from: now, to: start), "\(seconds) seconds before")
        }
        let quarter = GlanceCount.minutes.format(start.addingTimeInterval(-900) ..< start.addingTimeInterval(GlanceCount.started))
        #expect(quarter == "15 min")
        // In another language the unit would be another word beside the app's English one: the style is held to English.
        #expect(GlanceCount.minutes.locale == GlanceCount.english)
    }

    /// A link opens its page once, and the same link opened again is a
    /// second link.
    @Test func aLinkTheAppWasOpenedWithIsFollowedOnce() throws {
        let links = OpenedLinks()
        #expect(links.waiting == nil && links.take() == nil)
        links.open(TodayLinks.route(try #require(URL(string: "substrate-today://task/p-vendor"))))
        let first = try #require(links.take())
        #expect(first.route == .task("p-vendor") && links.waiting == nil)
        links.open(.task("p-vendor"))
        #expect(links.waiting != first && links.take()?.route == .task("p-vendor"))
        // The day's own link, and one this build does not know, open the day.
        links.open(TodayLinks.route(try #require(URL(string: "substrate-today://elsewhere"))))
        let day = try #require(links.take())
        #expect(day.route == nil)
    }
}
#endif
