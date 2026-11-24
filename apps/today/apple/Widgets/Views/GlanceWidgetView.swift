import SwiftUI
import TodayGlance

/// The two sizes the widgets come in: a row of the home screen, and several.
enum GlanceSize: Sendable {
    case medium, large

    /// The most rows a list draws: more than the size holds at the default
    /// text size on the largest iPhone, so what fits is what decides.
    var rows: Int {
        switch self {
        case .medium: 8
        case .large: 20
        }
    }
}

/// One of the three widgets at one instant of the day: everything it
/// draws inside the margins the system gives a widget. The extension puts
/// it on the system's background, and the gallery on a card of the same
/// size; both draw this one view. A widget has no name of its own over its
/// rows: it is rows from its first line.
struct GlanceWidgetView: View {
    var kind: GlanceKind
    var day: GlanceDay
    var size: GlanceSize

    var body: some View {
        Group {
            if let message = day.message {
                GlanceMessage(message: message)
            } else {
                switch kind {
                case .now: NowGlanceView(day: day, size: size)
                case .tasks: TasksGlanceView(day: day, size: size)
                case .meetings: MeetingsGlanceView(day: day, size: size)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        // A larger text size draws fewer rows; past this one a widget has no room for a row at all.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }
}
