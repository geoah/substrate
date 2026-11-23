import SwiftUI
import TodayGlance

/// The two sizes the widgets come in: a row of the home screen, and several.
enum GlanceSize: Sendable {
    case medium, large

    /// The most lines a list draws.
    var lines: Int {
        switch self {
        case .medium: 3
        case .large: 7
        }
    }

    /// Above and below a line's words.
    var linePadding: CGFloat {
        switch self {
        case .medium: 4
        case .large: 7
        }
    }

    /// Between a widget's name and its lines.
    var gap: CGFloat {
        switch self {
        case .medium: 4
        case .large: 6
        }
    }
}

/// One of the three widgets at one instant of the day: everything it
/// draws inside the margins the system gives a widget. The extension puts
/// it on the system's background, and the gallery on a card of the same
/// size; both draw this one view.
struct GlanceWidgetView: View {
    var kind: GlanceKind
    var day: GlanceDay
    var size: GlanceSize

    var body: some View {
        Group {
            if let message = day.message {
                VStack(alignment: .leading, spacing: size.gap) {
                    switch kind {
                    case .now: EmptyView()
                    case .tasks: GlanceHeader(glyph: .task, title: day.tasks.title)
                    case .meetings: GlanceHeader(glyph: .meeting, title: day.meetings.title)
                    }
                    GlanceMessage(message: message)
                }
            } else {
                switch kind {
                case .now: NowGlanceView(day: day, size: size)
                case .tasks: TasksGlanceView(day: day, size: size)
                case .meetings: MeetingsGlanceView(day: day, size: size)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        // A larger text size draws fewer lines; past this one a widget has no room for a line at all.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }
}
