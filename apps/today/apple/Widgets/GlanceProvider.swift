import Foundation
import TodayGlance
import WidgetKit

/// The day a widget draws from one instant until the next entry's.
struct GlanceEntry: TimelineEntry {
    var date: Date
    var day: GlanceDay
}

/// The widgets' timeline, read from the Keychain item the app wrote
/// (`GlanceKeychain`): the day that holds now and each one after it,
/// already made by the app, so nothing is worked out here. With no item,
/// or one that cannot be read, the one entry says to open the app.
struct GlanceProvider: TimelineProvider {
    /// What stands in a widget before its day is read: an invented day,
    /// which the system draws as blank shapes.
    func placeholder(in context: Context) -> GlanceEntry {
        GlanceEntry(date: .now, day: .sample(at: .now))
    }

    /// The widget gallery's picture is the invented day; anywhere else it
    /// is the day as it stands.
    func getSnapshot(in context: Context, completion: @escaping @Sendable (GlanceEntry) -> Void) {
        let now = Date.now
        completion(context.isPreview ? GlanceEntry(date: now, day: .sample(at: now)) : entries(from: now)[0])
    }

    /// The system asks again once the last entry is reached. Where there is
    /// only the one that holds now, nothing here can change it: the app
    /// has the widgets read again each time it writes the item.
    func getTimeline(in context: Context, completion: @escaping @Sendable (Timeline<GlanceEntry>) -> Void) {
        let entries = entries(from: .now)
        completion(Timeline(entries: entries, policy: entries.count > 1 ? .atEnd : .never))
    }

    /// The item's days from an instant on, each an entry at the instant it
    /// holds from. The first holds already, and the system draws it at once.
    func entries(from now: Date) -> [GlanceEntry] {
        (GlanceKeychain.timeline() ?? .closed(at: now)).days(from: now).map { GlanceEntry(date: $0.at, day: $0) }
    }
}
