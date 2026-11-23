import Foundation
import TodayGlance

/// The app's own links, which a widget's lines carry and the app is opened
/// with: `substrate-today://day`, `substrate-today://task/<id>`,
/// `substrate-today://meeting/<id>`, and the two pages a row of the day
/// opens, `substrate-today://plan` and `substrate-today://suggested`. A
/// link is the route of the page it opens, and the day has none.
public enum TodayLinks {
    /// The link that opens a page; nil is the day.
    public static func link(_ route: TodayRoute?) -> String {
        switch route {
        case let .task(id): link("task", id)
        case let .meeting(id): link("meeting", id)
        case .plan: link("plan", nil)
        case .suggested: link("suggested", nil)
        default: GlanceLinks.day
        }
    }

    /// The page a link opens. Nil is the day: the day's own link, and any
    /// link this build does not know.
    public static func route(_ url: URL) -> TodayRoute? {
        guard url.scheme?.lowercased() == GlanceLinks.scheme, let parts = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        // An id is everything past the first slash, whatever it holds.
        let id = String(parts.path.dropFirst())
        switch parts.host {
        case "task": return id.isEmpty ? nil : .task(id)
        case "meeting": return id.isEmpty ? nil : .meeting(id)
        case "plan": return .plan
        case "suggested": return .suggested
        default: return nil
        }
    }

    public static func route(_ link: String) -> TodayRoute? { URL(string: link).flatMap(route) }

    private static func link(_ page: String, _ id: String?) -> String {
        var parts = URLComponents()
        parts.scheme = GlanceLinks.scheme
        parts.host = page
        if let id {
            // A slash inside an id is written out, so the id stays one part of the path.
            var allowed = CharacterSet.urlPathAllowed
            allowed.remove("/")
            parts.percentEncodedPath = "/" + (id.addingPercentEncoding(withAllowedCharacters: allowed) ?? id)
        }
        return parts.string ?? GlanceLinks.day
    }
}
