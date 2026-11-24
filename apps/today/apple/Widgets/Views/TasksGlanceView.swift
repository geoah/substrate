import SwiftUI
import TodayGlance

/// The Tasks widget: today's open tasks in the order of the day, one row
/// each, with its flag as a stripe and its project where there is room.
struct TasksGlanceView: View {
    var day: GlanceDay
    var size: GlanceSize

    var body: some View {
        if day.tasks.items.isEmpty {
            GlanceMessage(message: day.tasks.empty)
        } else {
            GlanceList(
                rows: day.tasks.items.map { GlanceRowContent($0) }, most: size.rows, foot: day.tasks.standing, from: day.at, links: size == .large)
        }
    }
}
