import SwiftUI
import TodayGlance

/// The Now and next widget: what leads the day, the next fixed point with
/// its count within the hour, and how much is left. The large size adds
/// what follows, in the order of the day.
struct NowGlanceView: View {
    var day: GlanceDay
    var size: GlanceSize

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            head.glanceLink(day.now?.link ?? GlanceLinks.day, on: size == .large)
            if size == .medium { Spacer(minLength: 6) }
            foot.padding(.top, size == .large ? 12 : 0).layoutPriority(1)
            if size == .large, !day.upcoming.isEmpty {
                GlanceFitting(most: min(day.upcoming.count, size.lines)) { count in
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(day.upcoming.prefix(count)) { item in
                            UpcomingGlanceLine(item: item).padding(.vertical, size.linePadding).glanceLink(item.link, on: true)
                        }
                    }
                }
                .padding(.top, 12)
                .frame(maxHeight: .infinity, alignment: .top)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }

    /// What leads: its glyph, its title, and one line.
    @ViewBuilder private var head: some View {
        if let now = day.now {
            HStack(alignment: .top, spacing: GlanceLook.glyphGap) {
                GlanceGlyphView(glyph: now.glyph, leads: true).padding(.top, 1)
                VStack(alignment: .leading, spacing: 3) {
                    Text(now.title).glance(.lead).foregroundStyle(.glanceInk).lineLimit(2).privacySensitive()
                    if now.line != nil || now.count != nil {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            if let line = now.line {
                                Text(line).glance(.second).foregroundStyle(.glanceSecond).lineLimit(2).privacySensitive()
                            }
                            if let count = now.count { GlanceCount(to: count, from: day.at) }
                        }
                    }
                }
                .multilineTextAlignment(.leading)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .glanceHangingStripe(now.flag, hollow: now.suggested)
            .contentShape(Rectangle())
            .accessibilityElement(children: .combine)
            .accessibilityValue(now.flag?.word ?? "")
        } else if let quiet = day.quiet {
            Text(quiet).glance(.row).foregroundStyle(.glanceSecond)
        }
    }

    /// The next fixed point on one line, and under it what is left.
    private var foot: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let next = day.next {
                HStack(alignment: .firstTextBaseline, spacing: GlanceLook.glyphGap) {
                    GlanceGlyphView(glyph: .meeting)
                    Text(next.time).glance(.time).foregroundStyle(.glanceInk).fixedSize()
                    Text(next.title).glance(.row).foregroundStyle(.glanceInk).privacySensitive().frame(maxWidth: .infinity, alignment: .leading)
                    if next.counts { GlanceCount(to: next.start, from: day.at) }
                }
                .lineLimit(1)
                .contentShape(Rectangle())
                .accessibilityElement(children: .combine)
                .glanceLink(next.link, on: size == .large)
            }
            if let left = day.left {
                Text(left).glance(.quiet).foregroundStyle(.glanceSecond).lineLimit(1)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// One thing that follows, on one line: its glyph, a meeting's start, its
/// title, and a task's flag as its stripe.
struct UpcomingGlanceLine: View {
    var item: GlanceItem

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: GlanceLook.glyphGap) {
            GlanceGlyphView(glyph: item.glyph)
            if let time = item.time { Text(time).glance(.time).foregroundStyle(.glanceInk).fixedSize() }
            Text(item.title).glance(.row).foregroundStyle(.glanceInk).privacySensitive().frame(maxWidth: .infinity, alignment: .leading)
        }
        .lineLimit(1)
        .glanceHangingStripe(item.flag, hollow: item.suggested)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(item.flag?.word ?? "")
    }
}
