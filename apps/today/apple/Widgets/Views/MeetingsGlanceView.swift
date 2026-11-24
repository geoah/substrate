import SwiftUI
import TodayGlance

/// The Meetings widget: the meetings of the rest of today, one row each
/// with its start and its end. The one in progress says so, the next one
/// counts to its start within the hour, two that wait for a choice stand
/// on one band, and one the owner does not go to is struck through.
struct MeetingsGlanceView: View {
    var day: GlanceDay
    var size: GlanceSize

    var body: some View {
        if day.meetings.items.isEmpty {
            GlanceMessage(message: day.meetings.empty)
        } else {
            GlanceList(
                rows: day.meetings.items.map { GlanceRowContent($0) }, most: size.rows, foot: day.meetings.standing, from: day.at,
                links: size == .large)
        }
    }
}
