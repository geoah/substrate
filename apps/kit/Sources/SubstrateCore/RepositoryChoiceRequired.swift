import Foundation

/// A sign-in of a person who may open several repositories and named none
/// (decision 0274): the door's `409 conflict` with reason
/// `repository_required`, and the repositories it may open. The same sign-in
/// naming one of them, with the factors already given, signs in: the door
/// spent no code on the refusal.
public struct RepositoryChoiceRequired: LocalizedError, Sendable, Equatable {
    /// One repository the person may open: its id, which the retry names,
    /// and its name, which the pick shows.
    public struct Choice: Sendable, Equatable, Hashable, Identifiable {
        public let id: String
        public let name: String

        public init(id: String, name: String) {
            self.id = id
            self.name = name
        }
    }

    public let repositories: [Choice]

    public init(repositories: [Choice]) { self.repositories = repositories }

    /// The refusal a conflict's envelope carries, or nil when the conflict is
    /// another one.
    public init?(envelope data: Data) {
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let error = object["error"] as? [String: Any],
            error["reason"] as? String == "repository_required",
            let listed = error["repositories"] as? [[String: Any]]
        else { return nil }
        repositories = listed.compactMap { entry in
            guard let id = entry["id"] as? String, let name = entry["name"] as? String else { return nil }
            return Choice(id: id, name: name)
        }
        if repositories.isEmpty { return nil }
    }

    public var errorDescription: String? { "You may open more than one repository: choose the one to sign in to." }
}
