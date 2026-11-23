import SwiftUI
import TodayGlance

/// The Tasks widget: today's open tasks in the order of the day, each with
/// its flag as a stripe, its title, and its project where there is room.
struct TasksGlanceView: View {
    var day: GlanceDay
    var size: GlanceSize

    var body: some View {
        VStack(alignment: .leading, spacing: size.gap) {
            GlanceHeader(glyph: .task, title: day.tasks.title, standing: day.tasks.standing)
            if day.tasks.items.isEmpty {
                Text(day.tasks.empty).glance(.row).foregroundStyle(.glanceSecond)
            } else {
                GlanceFitting(most: min(day.tasks.items.count, size.lines)) { count in
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(day.tasks.items.prefix(count)) { item in
                            TaskGlanceLine(item: item).padding(.vertical, size.linePadding).glanceLink(item.link, on: size == .large)
                        }
                        if let more = day.tasks.more(after: count) {
                            Text(more).glance(.quiet).foregroundStyle(.glanceSecond).padding(.top, size.linePadding)
                                .padding(.leading, GlanceLook.stripeRoom)
                        }
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

/// One task on one line.
struct TaskGlanceLine: View {
    var item: GlanceItem

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if item.glyph == .urgent {
                Image(systemName: item.glyph.symbol).glance(.second).foregroundStyle(GlanceLook.urgent.color).accented().accessibilityHidden(true)
            }
            // The project is said where the title leaves it room, and not at the title's cost.
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .firstTextBaseline, spacing: 10) {
                    title.fixedSize()
                    Spacer(minLength: 0)
                    if let project = item.project {
                        Text(project).glance(.quiet).foregroundStyle(.glanceSecond).privacySensitive().fixedSize()
                    }
                }
                title.frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .lineLimit(1)
        .glanceStripe(item.flag, hollow: item.suggested)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityValue(item.flag?.word ?? "")
    }

    private var title: some View {
        Text(item.title).glance(.row).foregroundStyle(.glanceInk).privacySensitive()
    }
}
