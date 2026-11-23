import SwiftUI
import TodayGlance

/// The Meetings widget: the meetings of the rest of today. The one in
/// progress says so, the next one counts to its start within the hour, two
/// that wait for a choice stand on one band, and one the owner does not go
/// to is struck through.
struct MeetingsGlanceView: View {
    var day: GlanceDay
    var size: GlanceSize

    var body: some View {
        VStack(alignment: .leading, spacing: size.gap) {
            GlanceHeader(glyph: .meeting, title: day.meetings.title, standing: day.meetings.standing)
            if day.meetings.items.isEmpty {
                Text(day.meetings.empty).glance(.row).foregroundStyle(.glanceSecond)
            } else {
                GlanceFitting(most: min(day.meetings.items.count, size.lines)) { count in
                    VStack(alignment: .leading, spacing: 0) {
                        let shown = Array(day.meetings.items.prefix(count))
                        ForEach(Array(shown.enumerated()), id: \.element.id) { index, meeting in
                            MeetingGlanceLine(meeting: meeting, from: day.at)
                                .padding(.vertical, size.linePadding)
                                // A pair whose second has no room is still one whole band.
                                .background { MeetingGlanceBand(pair: meeting.pair, alone: meeting.pair == .first && index == shown.count - 1) }
                                .glanceLink(meeting.link, on: size == .large)
                        }
                        if let more = day.meetings.more(after: count) {
                            Text(more).glance(.quiet).foregroundStyle(.glanceSecond).padding(.top, size.linePadding)
                        }
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

/// One meeting on one line: its start, its title, and what ends the line.
struct MeetingGlanceLine: View {
    var meeting: GlanceMeeting
    /// The instant the day it is drawn in holds from.
    var from: Date

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            // Every start takes the room of the widest, so the titles line up.
            Text(verbatim: "00:00").hidden()
                .overlay(alignment: .leading) { Text(meeting.time).fixedSize() }
                .glance(.time)
                .foregroundStyle(meeting.off ? .glanceSecond : .glanceInk)
            Text(meeting.title)
                .glance(.row)
                .foregroundStyle(meeting.off ? .glanceSecond : .glanceInk)
                .strikethrough(meeting.off)
                .privacySensitive()
                .frame(maxWidth: .infinity, alignment: .leading)
            if meeting.counts {
                GlanceCount(to: meeting.start, from: from)
            } else if let note = meeting.note {
                Text(note).glance(.quiet)
                    .foregroundStyle(meeting.pair != nil && !meeting.live ? GlanceLook.cautionWords.color : .glanceInk)
                    .fixedSize()
            }
        }
        .lineLimit(1)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }
}

/// What marks two meetings that share time while no choice is made: one
/// band behind both lines, a little wider than the lines themselves.
struct MeetingGlanceBand: View {
    @Environment(\.glanceFlat) private var flat
    var pair: GlanceMeeting.Pair?
    var alone = false

    var body: some View {
        if let pair {
            let (top, bottom): (CGFloat, CGFloat) = alone ? (8, 8) : pair == .first ? (8, 0) : (0, 8)
            UnevenRoundedRectangle(
                topLeadingRadius: top, bottomLeadingRadius: bottom, bottomTrailingRadius: bottom, topTrailingRadius: top, style: .continuous
            )
            .fill(GlanceLook.cautionFill.color.opacity(flat ? 0.25 : 1))
            .padding(.horizontal, -8)
        }
    }
}
