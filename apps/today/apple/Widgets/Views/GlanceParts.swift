import SwiftUI
import TodayGlance

/// A kind's glyph in the box lines are lined up on, as the app draws it:
/// bare, in ink, and the urgent flame in the urgent red.
struct GlanceGlyphView: View {
    @Environment(\.glanceScale) private var scale
    var glyph: GlanceGlyph
    /// The current item's is larger and in ink; a line's is quiet.
    var leads = false

    var body: some View {
        Image(systemName: glyph.symbol)
            .font(.system(size: (leads ? 19 : 14) * scale, weight: .regular))
            .foregroundStyle(glyph == .urgent ? GlanceLook.urgent.color : leads ? .glanceInk : .glanceSecond)
            .frame(width: GlanceLook.glyphBox * scale, alignment: .center)
            .accented()
            .accessibilityHidden(true)
    }
}

/// The only mark of a flag: a stripe in its colour, hollow for one the
/// assistant suggests. Where the system draws the widget in one tint the
/// colour is gone, and High is the most solid of the three.
struct GlanceStripe: View {
    @Environment(\.glanceFlat) private var flat
    var flag: GlanceFlag
    var hollow = false

    var body: some View {
        let colour = GlanceLook.flag(flag).color.opacity(flat ? solidity : 1)
        Group {
            if hollow {
                Capsule().strokeBorder(colour, lineWidth: 1.25).frame(width: GlanceLook.stripeWidth + 1)
            } else {
                Capsule().fill(colour).frame(width: GlanceLook.stripeWidth)
            }
        }
        .accented()
        .accessibilityHidden(true)
    }

    private var solidity: Double {
        switch flag {
        case .high: 1
        case .medium: 0.6
        case .low: 0.3
        }
    }
}

extension View {
    /// A flag's stripe down the leading edge of a line, with the line moved
    /// in to make room for it. A line with no flag keeps the room, so the
    /// titles of a list stand on one edge.
    func glanceStripe(_ flag: GlanceFlag?, hollow: Bool) -> some View {
        padding(.leading, GlanceLook.stripeRoom)
            .overlay(alignment: .leading) {
                if let flag { GlanceStripe(flag: flag, hollow: hollow).padding(.vertical, 2) }
            }
    }

    /// A flag's stripe hung in the widget's margin beside a line that keeps
    /// its place, as the app hangs the stripe of the row that leads: where
    /// lines with a flag and lines without one stand in one list, their
    /// glyphs and their words stay on one edge.
    func glanceHangingStripe(_ flag: GlanceFlag?, hollow: Bool) -> some View {
        overlay(alignment: .leading) {
            if let flag { GlanceStripe(flag: flag, hollow: hollow).padding(.vertical, 2).offset(x: -GlanceLook.stripeHang) }
        }
    }
}

/// The count to a meeting's start, in minutes: "in 12 min". The system
/// counts the minutes itself, so the count moves on the home screen with no
/// new day for each one. In a picture it stands at the day's own instant,
/// in the core's words, which are the same words.
struct GlanceCount: View {
    /// The minutes left as the system says them: "12 min". In English, as
    /// every word of a widget is.
    static let minutes = Date.ComponentsFormatStyle(style: .abbreviated, locale: english, fields: [.minute])
    static let english = Locale(identifier: "en")
    /// The system drops a minute that has started, and the app counts it:
    /// "in 12 min" until twelve whole minutes are left. So the stretch the
    /// system counts runs this much past the start.
    static let started: TimeInterval = 59.9

    @Environment(\.glancePaused) private var paused
    var to: Date
    /// The instant the day it is drawn in holds from.
    var from: Date

    var body: some View {
        // The widest it gets keeps its room, so the line does not move as the minutes go.
        Text(verbatim: "\(GlanceWords.until) 00 min").hidden()
            .overlay(alignment: .trailing) {
                words.multilineTextAlignment(.trailing).frame(maxWidth: .infinity, alignment: .trailing)
            }
            .glance(.quiet)
            .foregroundStyle(.glanceInk)
            .lineLimit(1)
            .fixedSize()
            .environment(\.locale, Self.english)
    }

    private var words: Text {
        if paused { return Text(verbatim: GlanceWords.count(from: from, to: to)) }
        return Text("\(GlanceWords.until) \(.dateRange(endingAt: to.addingTimeInterval(Self.started)), format: Self.minutes)")
    }
}

/// A widget's own name over its lines, and how the day stands beside it.
struct GlanceHeader: View {
    var glyph: GlanceGlyph
    var title: String
    var standing: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: glyph.symbol).glance(.label).foregroundStyle(.glanceInk).accented().accessibilityHidden(true)
            Text(title).glance(.label).foregroundStyle(.glanceInk)
            Spacer(minLength: 8)
            if let standing { Text(standing).glance(.quiet).foregroundStyle(.glanceSecond) }
        }
        .lineLimit(1)
    }
}

/// As many lines as fit, and one quiet line for the rest. A larger text
/// size fits fewer; the count each attempt is given is how many it draws.
struct GlanceFitting<Line: View>: View {
    /// The most lines the size draws.
    var most: Int
    @ViewBuilder var lines: (Int) -> Line

    var body: some View {
        ViewThatFits(in: .vertical) {
            ForEach(Array(stride(from: max(1, most), through: 1, by: -1)), id: \.self) { count in
                lines(count)
            }
        }
    }
}

/// What a widget says in place of the day, under its own name: nothing to
/// draw one from, signed out, or a day that is out of date.
struct GlanceMessage: View {
    var message: String

    var body: some View {
        Text(message)
            .glance(.row)
            .foregroundStyle(.glanceSecond)
            .multilineTextAlignment(.leading)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension View {
    /// A line that opens its own page where the widget's size takes taps
    /// on its lines; elsewhere a tap anywhere opens the day.
    @ViewBuilder func glanceLink(_ link: String, on: Bool) -> some View {
        if on, let url = URL(string: link) {
            Link(destination: url) { self }
        } else {
            self
        }
    }
}
