import Foundation

/// The server's own refusal: the error envelope's code, message and
/// problems, shown to the owner as they are.
public struct ServerError: LocalizedError, Sendable, Equatable {
    /// One problem as the envelope's `problemDetails` splits it: the input
    /// it concerns, so a client acts on the input without reading the words.
    public struct ProblemDetail: Sendable, Equatable {
        /// `props.revision`; empty where the problem names no input.
        public let path: String
        public let message: String

        public init(path: String, message: String) {
            self.path = path
            self.message = message
        }
    }

    public let status: Int
    public let code: String
    public let message: String
    public let problems: [String]
    public let problemDetails: [ProblemDetail]

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
        problemDetails = ((error["problemDetails"] as? [[String: Any]]) ?? []).compactMap { detail in
            (detail["message"] as? String).map { ProblemDetail(path: (detail["path"] as? String) ?? "", message: $0) }
        }
    }

    public init(
        status: Int, code: String = "", message: String, problems: [String] = [], problemDetails: [ProblemDetail] = []
    ) {
        self.status = status
        self.code = code
        self.message = message
        self.problems = problems
        self.problemDetails = problemDetails
    }

    /// A refusal the same request would meet again: a 4xx other than a
    /// conflict or a rate limit, which clear on their own.
    public var isPermanent: Bool { (400 ..< 500).contains(status) && status != 409 && status != 429 }
}
