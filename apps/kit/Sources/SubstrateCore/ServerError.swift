import Foundation

/// The server's own refusal: the error envelope's code, message and
/// problems, shown to the owner as they are.
public struct ServerError: LocalizedError, Sendable, Equatable {
    public let status: Int
    public let code: String
    public let message: String
    public let problems: [String]

    public var text: String { ([message] + problems).joined(separator: "\n") }
    public var errorDescription: String? { text }

    /// The envelope, or nil when the body is not one: then the answer may be
    /// a gateway's, and says nothing about whether the request took effect.
    public init?(status: Int, envelope data: Data) {
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let error = object["error"] as? [String: Any],
            let message = error["message"] as? String
        else { return nil }
        self.status = status
        code = (error["code"] as? String) ?? ""
        self.message = message
        problems = (error["problems"] as? [String]) ?? []
    }

    public init(status: Int, code: String = "", message: String, problems: [String] = []) {
        self.status = status
        self.code = code
        self.message = message
        self.problems = problems
    }

    /// A refusal the same request would meet again: a 4xx other than a
    /// conflict or a rate limit, which clear on their own.
    public var isPermanent: Bool { (400 ..< 500).contains(status) && status != 409 && status != 429 }
}
