import SwiftUI
import TodayGlance
import WidgetKit

/// The widget extension: the three home-screen widgets of Substrate Today.
/// It reads the day the app wrote ahead and draws it; it reads no record
/// and reaches no server.
@main
struct SubstrateTodayWidgets: WidgetBundle {
    var body: some Widget {
        NowWidget()
        TasksWidget()
        MeetingsWidget()
    }
}

/// What leads the day now, the next fixed point, and what follows.
struct NowWidget: Widget {
    var body: some WidgetConfiguration { GlanceConfiguration.make(.now) }
}

/// Today's open tasks, in the order of the day.
struct TasksWidget: Widget {
    var body: some WidgetConfiguration { GlanceConfiguration.make(.tasks) }
}

/// The meetings of the rest of today.
struct MeetingsWidget: Widget {
    var body: some WidgetConfiguration { GlanceConfiguration.make(.meetings) }
}

/// One widget's configuration: its kind, its name and description in the
/// widget gallery, and the two sizes that take a row of the home screen or
/// several. No widget is offered small.
enum GlanceConfiguration {
    @MainActor static func make(_ kind: GlanceKind) -> some WidgetConfiguration {
        StaticConfiguration(kind: kind.rawValue, provider: GlanceProvider()) { entry in
            GlanceWidgetEntryView(kind: kind, entry: entry)
        }
        .configurationDisplayName(kind.name)
        .description(kind.summary)
        .supportedFamilies([.systemMedium, .systemLarge])
    }
}

/// A widget as the system shows it: the shared view on the app's card
/// colour, inside the margins the system gives. A tap anywhere opens the
/// day; in the large size a line opens its own page.
struct GlanceWidgetEntryView: View {
    @Environment(\.widgetFamily) private var family
    @Environment(\.widgetRenderingMode) private var rendering
    var kind: GlanceKind
    var entry: GlanceEntry

    var body: some View {
        GlanceWidgetView(kind: kind, day: entry.day, size: family == .systemLarge ? .large : .medium)
            .environment(\.glanceFlat, rendering != .fullColor)
            .containerBackground(for: .widget) { GlanceLook.card.color }
            .widgetURL(URL(string: GlanceLinks.day))
    }
}
