import SwiftUI
import TodayGlance

/// A kind's glyph in the box rows are lined up on, as the app draws it:
/// bare, quiet, in ink on the row that leads, and the urgent flame in the
/// urgent red.
struct GlanceGlyphView: View {
    @Environment(\.glanceScale) private var scale
    var glyph: GlanceGlyph
    var leads = false

    var body: some View {
        // The box is as tall as a row's words and no taller, whatever the glyph: every row is one height.
        Text(verbatim: " ").glance(.row).hidden()
            .frame(width: GlanceLook.glyphBox * scale)
            .overlay {
                Image(systemName: glyph.symbol)
                    .font(.system(size: GlanceLook.glyphSize * scale, weight: leads ? .semibold : .regular))
                    .foregroundStyle(glyph == .urgent ? GlanceLook.urgent.color : leads ? .glanceInk : .glanceSecond)
                    .fixedSize()
                    .accented()
            }
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
    /// A flag's stripe hung in the widget's margin beside a row that keeps
    /// its place, as the app hangs the stripe of the row that leads.
    func glanceHangingStripe(_ flag: GlanceFlag?, hollow: Bool) -> some View {
        overlay(alignment: .leading) {
            if let flag { GlanceStripe(flag: flag, hollow: hollow).padding(.vertical, 2.5).offset(x: -GlanceLook.stripeHang) }
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
        // The widest it gets keeps its room, so the row does not move as the minutes go.
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

// MARK: A row

/// One thing of the day as its row draws it. A row is one line: a flag's
/// stripe, the kind's glyph, a meeting's times, the title, and what ends it.
struct GlanceRowContent: Identifiable {
    var id: String
    var glyph: GlanceGlyph
    var title: String
    /// A meeting's start, and its end.
    var time: String?
    var until: String?
    /// The times keep the room of the widest, so a list of meetings has
    /// its titles on one edge. Elsewhere the end gives way to the title.
    var timesKeepRoom = false
    var flag: GlanceFlag?
    var hollow = false
    /// The row that leads the day: its title is heavier and its glyph in ink.
    var leads = false
    /// A meeting the owner does not go to.
    var struck = false
    /// What is said quietly at the end where the title leaves it room: the
    /// first that fits.
    var asides: [String] = []
    /// What is always said at the end: "now", "Choose one".
    var note: String?
    var caution = false
    /// The instant a running count of minutes ends at.
    var count: Date?
    var pair: GlanceMeeting.Pair?
    var link: String
}

extension GlanceRowContent {
    /// A task, a routine, a meeting or anything else of the day's order.
    init(_ item: GlanceItem, leads: Bool = false) {
        let meeting = item.glyph == .meeting
        self.init(
            id: item.id, glyph: item.glyph, title: item.title, time: item.time, until: item.until, flag: item.flag, hollow: item.suggested,
            leads: leads, asides: meeting ? [] : [item.line, item.project].compactMap { $0 }, note: meeting ? item.line : nil, count: item.count,
            link: item.link)
    }

    /// A meeting in a list of meetings.
    init(_ meeting: GlanceMeeting) {
        self.init(
            id: "meeting:" + meeting.id, glyph: .meeting, title: meeting.title, time: meeting.time, until: meeting.until, timesKeepRoom: true,
            leads: meeting.live, struck: meeting.off, note: meeting.note, caution: meeting.pair != nil && !meeting.live,
            count: meeting.counts ? meeting.start : nil, pair: meeting.pair, link: meeting.link)
    }
}

struct GlanceRow: View {
    @Environment(\.glanceScale) private var scale
    var row: GlanceRowContent
    /// The instant the day it is drawn in holds from.
    var from: Date

    /// How much of a row is drawn: the most that fits is.
    private struct Shape: Hashable {
        var aside: String?
        var until: Bool
        /// The title takes the room there is and is cut short at its end.
        var cut = false
    }

    private var shapes: [Shape] {
        var out = row.asides.map { Shape(aside: $0, until: true) }
        out.append(Shape(until: true))
        if row.until != nil, !row.timesKeepRoom { out.append(Shape(until: false)) }
        out.append(Shape(until: row.timesKeepRoom, cut: true))
        return out
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: GlanceLook.glyphGap) {
            GlanceGlyphView(glyph: row.glyph, leads: row.leads)
            ViewThatFits(in: .horizontal) {
                ForEach(shapes, id: \.self) { shape in line(shape) }
            }
        }
        .lineLimit(1)
        .padding(.vertical, GlanceLook.rowPadding)
        .glanceHangingStripe(row.flag, hollow: row.hollow)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(row.flag?.word ?? "")
    }

    private func line(_ shape: Shape) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: GlanceLook.glyphGap) {
            if let time = row.time { times(time, until: shape.until ? row.until : nil) }
            if shape.cut {
                title.frame(maxWidth: .infinity, alignment: .leading)
            } else {
                title.fixedSize()
                Spacer(minLength: 2)
            }
            if let aside = shape.aside {
                Text(aside).glance(.quiet).foregroundStyle(.glanceSecond).privacySensitive().fixedSize()
            }
            if let count = row.count {
                GlanceCount(to: count, from: from)
            } else if let note = row.note {
                Text(note).glance(.quiet).foregroundStyle(row.caution ? GlanceLook.cautionWords.color : .glanceInk).fixedSize()
            }
        }
    }

    private var title: some View {
        Text(row.title)
            .glance(row.leads ? .lead : .row)
            .foregroundStyle(row.struck ? .glanceSecond : .glanceInk)
            .strikethrough(row.struck)
            .privacySensitive()
    }

    /// A meeting's start, and its end after it: "14:20–14:40".
    private func times(_ start: String, until: String?) -> some View {
        let ink: Color = row.struck ? .glanceSecond : .glanceInk
        let range =
            Text(start).font(GlanceLook.font(.time, scale: scale)).foregroundStyle(ink)
            + Text(until.map { "–" + $0 } ?? "").font(GlanceLook.font(.until, scale: scale)).foregroundStyle(Color.glanceSecond)
        return Group {
            if row.timesKeepRoom {
                // Every range takes the room of the widest, so the titles line up.
                Text(verbatim: "00:00–00:00").font(GlanceLook.font(.time, scale: scale)).hidden()
                    .overlay(alignment: .leading) { range.fixedSize() }
            } else {
                range.fixedSize()
            }
        }
    }
}

/// What marks two meetings that share time while no choice is made: one
/// band behind both rows, a little wider than the rows themselves.
struct GlancePairBand: View {
    @Environment(\.glanceFlat) private var flat
    var pair: GlanceMeeting.Pair?
    /// The other of the two has no room: the band is whole around this one.
    var alone = false

    var body: some View {
        if let pair {
            let (top, bottom): (CGFloat, CGFloat) = alone ? (6, 6) : pair == .first ? (6, 0) : (0, 6)
            UnevenRoundedRectangle(
                topLeadingRadius: top, bottomLeadingRadius: bottom, bottomTrailingRadius: bottom, topTrailingRadius: top, style: .continuous
            )
            .fill(GlanceLook.cautionFill.color.opacity(flat ? 0.25 : 1))
            .padding(.horizontal, -6)
        }
    }
}

// MARK: A list

/// A widget's rows: as many as fit, and under them one quiet line where the
/// rows leave it room. A larger text size fits fewer rows, and none is cut
/// short of its height.
struct GlanceList: View {
    var rows: [GlanceRowContent]
    /// The most rows the size draws.
    var most: Int
    /// How the day stands, said last where a line is left for it.
    var foot: String?
    /// The instant the day holds from.
    var from: Date
    /// A row opens its own page; elsewhere a tap anywhere opens the day.
    var links: Bool
    /// The row the list reaches whatever fits: the next fixed point. Where
    /// it would fall below the last row, the rows before it give way, and
    /// one quiet row says how many ("and 7 more").
    var reach: String?

    private struct Attempt: Hashable {
        var count: Int
        var foot: Bool
    }

    private enum Entry: Identifiable {
        case row(GlanceRowContent, alone: Bool)
        case gap(String)

        var id: String {
            switch self {
            case let .row(row, _): row.id
            case .gap: "gap"
            }
        }
    }

    /// The tallest first, so the one drawn is the most that fits.
    private var attempts: [Attempt] {
        stride(from: max(1, min(rows.count, most)), through: 1, by: -1).flatMap { count in
            foot == nil ? [Attempt(count: count, foot: false)] : [Attempt(count: count, foot: true), Attempt(count: count, foot: false)]
        }
    }

    /// What `count` rows are: the first of the list, or fewer of them and
    /// then the row the list reaches.
    private func entries(_ count: Int) -> [Entry] {
        var shown = Array(rows.prefix(count))
        var gap: (at: Int, words: String)?
        if count >= 3, let reach, let index = rows.firstIndex(where: { $0.id == reach }), index >= count,
            let words = GlanceWords.more(index - (count - 2))
        {
            shown = Array(rows.prefix(count - 2)) + [rows[index]]
            gap = (count - 2, words)
        }
        var out: [Entry] = shown.enumerated().map { index, row in
            // A pair whose second has no room is still one whole band.
            .row(row, alone: row.pair == .first && (index == shown.count - 1 || shown[index + 1].pair != .second))
        }
        if let gap { out.insert(.gap(gap.words), at: gap.at) }
        return out
    }

    var body: some View {
        ViewThatFits(in: .vertical) {
            ForEach(attempts, id: \.self) { attempt in
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(entries(attempt.count)) { entry in
                        switch entry {
                        case let .row(row, alone):
                            GlanceRow(row: row, from: from)
                                .background { GlancePairBand(pair: row.pair, alone: alone) }
                                .glanceLink(row.link, on: links)
                        case let .gap(words):
                            GlanceQuietLine(words: words).padding(.vertical, GlanceLook.rowPadding)
                        }
                    }
                    if attempt.foot, let foot {
                        Spacer(minLength: 0)
                        Text(foot).glance(.quiet).foregroundStyle(.glanceSecond).lineLimit(1).padding(.top, GlanceLook.rowPadding)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

/// A quiet line among the rows, its words on the rows' own edge.
struct GlanceQuietLine: View {
    @Environment(\.glanceScale) private var scale
    var words: String

    var body: some View {
        Text(words).glance(.quiet).foregroundStyle(.glanceSecond).lineLimit(1)
            .padding(.leading, (GlanceLook.glyphBox + GlanceLook.glyphGap) * scale)
    }
}

/// What a widget says in place of its rows: nothing to draw a day from,
/// signed out, a day that is out of date, or nothing left of its kind.
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
    /// A row that opens its own page where the widget's size takes taps
    /// on its rows; elsewhere a tap anywhere opens the day.
    @ViewBuilder func glanceLink(_ link: String, on: Bool) -> some View {
        if on, let url = URL(string: link) {
            Link(destination: url) { self }
        } else {
            self
        }
    }
}
