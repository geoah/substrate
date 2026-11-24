import SwiftUI
import TodayGlance

/// The Now and next widget: the day in its order, one row each. What leads
/// is first and heavier, what follows is under it, and the next fixed
/// point is always among the rows, with its count within the hour.
struct NowGlanceView: View {
    var day: GlanceDay
    var size: GlanceSize

    private var rows: [GlanceRowContent] {
        (day.now.map { [GlanceRowContent($0, leads: true)] } ?? []) + day.upcoming.map { GlanceRowContent($0) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: GlanceLook.rowPadding) {
            if let quiet = day.quiet { GlanceMessage(message: quiet) }
            if !rows.isEmpty || day.quiet == nil {
                GlanceList(
                    rows: rows, most: size.rows, foot: day.left, from: day.at, links: size == .large, reach: day.next.map { "meeting:" + $0.id })
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}
