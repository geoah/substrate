import SwiftUI
import WidgetKit

/// The widget extension holds the session's Live Activity and nothing else
/// (DESIGN.md §2.1). V1.1's AlarmKit countdown view joins it here.
@main
struct SubstrateWorkoutsWidgets: WidgetBundle {
    var body: some Widget {
        SessionLiveActivity()
    }
}
