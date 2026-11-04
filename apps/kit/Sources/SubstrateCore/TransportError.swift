import Foundation

/// The connection failed, or an answer came that was not the server's (a
/// proxy's page, a body that is not the envelope): whether the request took
/// effect is unknown, unlike a `ServerError`.
public struct TransportError: LocalizedError, Sendable, Equatable {
    /// The HTTP status, when an answer came at all.
    public let status: Int?
    public let text: String
    public var errorDescription: String? { text }

    public init(status: Int? = nil, text: String) {
        self.status = status
        self.text = text
    }

    /// An answer that is not the envelope, in words: its body where it is
    /// short plain text, never a proxy's HTML page, which says nothing a
    /// person can use.
    public init(status: Int, body: Data) {
        let text = String(data: body, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let plain = !text.isEmpty && text.count < 300 && !text.hasPrefix("<") && !text.lowercased().contains("<html")
        self.init(status: status, text: plain ? "The server answered \(status): \(text)" : "The server answered \(status).")
    }
}
