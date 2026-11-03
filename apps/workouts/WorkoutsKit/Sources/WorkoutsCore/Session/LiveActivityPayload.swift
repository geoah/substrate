import Foundation

/// The Live Activity's content as the ongoing-activity service carries it:
/// the presenter's content, the controller's revision, and, after Finish,
/// the summary the activity ends with. The app's widget side reads it into
/// its own content state.
public struct LiveActivityPayload: Codable, Equatable, Sendable {
    public var content: LiveActivityContent
    public var revision: Int
    public var finishedSummary: String?

    public init(content: LiveActivityContent, revision: Int, finishedSummary: String? = nil) {
        self.content = content
        self.revision = revision
        self.finishedSummary = finishedSummary
    }

    /// The countdown's end, so the widget flips to "Rest over" then with no
    /// app code running (§9.1). A finished session is never stale.
    public var staleAt: Date? { finishedSummary == nil ? content.countdown?.endsAt : nil }

    public func encoded() -> Data {
        // A plain value of strings, numbers and dates: it always encodes.
        (try? JSONEncoder().encode(self)) ?? Data()
    }

    public static func decoded(_ data: Data) -> LiveActivityPayload? {
        try? JSONDecoder().decode(LiveActivityPayload.self, from: data)
    }
}
