import Foundation

/// What an app token may do, from its scopes, matched as the server matches
/// them (internal/scope): `read:` and `write:` over kinds, `write` including
/// read; `agents:` and `decide:` over agent ids; `webhook:` one trigger. A
/// glob never reaches an internal kind, no scope reaches the auth kinds,
/// and none writes core or llm.
public struct Grants: Codable, Hashable, Sendable {
    public var scopes: [String]

    public init(_ scopes: [String]) {
        self.scopes = scopes
    }

    /// The four core kinds that hold the repository's own auth material.
    static let authKinds = Set(["token", "credential", "secret", "recoverykey"].map { "substrate.reamde.dev/core/\($0)" })
    /// The packages no scope writes.
    static let unwritable = ["substrate.reamde.dev/core/", "substrate.reamde.dev/llm/"]
    /// The seeded packages whose every kind is internal, for a caller that
    /// does not know a kind's declared purpose.
    static let machinery = ["substrate.reamde.dev/core/", "substrate.reamde.dev/llm/", "substrate.reamde.dev/console/"]

    /// Whether the grant reads records of `kind`. `purpose` is the kind's
    /// declared purpose where the caller read it; without it, a kind of the
    /// seeded core, llm and console packages counts as internal, which every
    /// one of them is.
    public func reads(_ kind: String, purpose: String? = nil) -> Bool {
        let internalKind = Self.isInternal(kind, purpose: purpose)
        return parsed.contains { ($0.verb == "read" || $0.verb == "write") && Self.matches($0.pattern, kind, internalKind) }
    }

    public func writes(_ kind: String, purpose: String? = nil) -> Bool {
        guard !Self.unwritable.contains(where: kind.hasPrefix) else { return false }
        let internalKind = Self.isInternal(kind, purpose: purpose)
        return parsed.contains { $0.verb == "write" && Self.matches($0.pattern, kind, internalKind) }
    }

    /// Whether the grant talks to the agent whose record id is `agent`.
    public func talksTo(_ agent: String) -> Bool { matchesAgent("agents", agent) }

    /// Whether the grant decides what the agent whose record id is `agent`
    /// proposes.
    public func decides(_ agent: String) -> Bool { matchesAgent("decide", agent) }

    /// Whether the grant delivers to the webhook trigger `trigger` and reads
    /// its parked deliveries.
    public func delivers(_ trigger: String) -> Bool {
        !trigger.isEmpty && parsed.contains { $0.verb == "webhook" && $0.pattern == trigger }
    }

    /// Whether the grant holds any `agents:` scope.
    public var talks: Bool { parsed.contains { $0.verb == "agents" } }

    /// Whether the grant holds any `decide:` scope.
    public var decidesAny: Bool { parsed.contains { $0.verb == "decide" } }

    /// The approved access as the console's everyday sentences, one a scope.
    public var words: [String] { scopes.map { ScopeWords.sentence($0) } }

    var parsed: [(verb: String, pattern: String)] { scopes.map(ScopeWords.split) }

    private func matchesAgent(_ verb: String, _ agent: String) -> Bool {
        !agent.isEmpty && parsed.contains { $0.verb == verb && Self.matchesGlob($0.pattern, agent) }
    }

    static func isInternal(_ kind: String, purpose: String?) -> Bool {
        purpose.map { $0 == "internal" } ?? machinery.contains(where: kind.hasPrefix)
    }

    /// One pattern against one kind: the auth kinds are never reached, and a
    /// glob never reaches an internal kind.
    static func matches(_ pattern: String, _ kind: String, _ internalKind: Bool) -> Bool {
        if authKinds.contains(kind) { return false }
        if isGlob(pattern) { return !internalKind && matchesGlob(pattern, kind) }
        return pattern == kind
    }

    static func isGlob(_ pattern: String) -> Bool { pattern == "*" || pattern.hasSuffix("/*") }

    /// `*`, a prefix up to its trailing `/`, or one exact name.
    static func matchesGlob(_ pattern: String, _ name: String) -> Bool {
        if pattern == "*" { return true }
        if pattern.hasSuffix("/*") { return name.hasPrefix(pattern.dropLast()) }
        return pattern == name
    }
}

/// A kind a scope reaches, as the approval page lists it: its reference,
/// its label where it declares one, and its purpose where it is not
/// `primary`.
public struct ScopeKind: Hashable, Sendable {
    public var identity: String
    public var plural: String?
    public var purpose: String?

    public init(identity: String, plural: String? = nil, purpose: String? = nil) {
        self.identity = identity
        self.plural = plural
        self.purpose = purpose
    }
}

/// A scope as the sentence the console's approval page shows beside its
/// checkbox (web/console's app-scopes.ts, whose words this mirrors): what the
/// app may do, over the kinds or agents it reaches, by their labels. Every
/// power stays in the words: a write may delete, a glob reaches what is
/// added later, talking to an agent reaches whatever the agent may do and
/// reads every chat with it, and a decision may edit the change it applies.
public enum ScopeWords {
    /// How many kind labels one sentence names before it counts the rest.
    static let maxLabels = 6

    /// The sentence for one scope. `kinds` are the kinds it reaches today,
    /// where the caller knows them; without them a pattern reads by its
    /// shape. `agents` are the names of the agents an agent scope matches.
    public static func sentence(_ scope: String, kinds: [ScopeKind] = [], agents: [String] = [],
                                packageName: String? = nil) -> String
    {
        let (verb, pattern) = split(scope)
        let glob = Grants.isGlob(pattern)
        let shape = Shape(pattern)
        switch verb {
        case "read", "write":
            let action = verb == "read" ? "See" : "See, add, change and delete"
            let later = glob ? ", including kinds added later" : ""
            if case .all = shape { return "\(action) all your records\(later)" }
            let what = kinds.isEmpty ? patternPhrase(shape) : kindsPhrase(kinds)
            return "\(action) your \(what)\(later)"
        case "agents":
            let later = glob ? ", including agents added later" : ""
            let who: String
            if case .all = shape { who = "all your agents" } else { who = agentsPhrase(shape, pattern, agents) }
            let them = glob ? "them" : "it"
            return "Talk to \(who)\(later), and through a chat have \(them) do anything \(glob ? "they" : "it") may do; "
                + "read every chat with \(them), yours included, answer \(glob ? "their" : "its") questions, "
                + "and rename or stop any of those chats"
        case "decide":
            let later = glob ? ", including agents added later" : ""
            return "Apply or dismiss the changes \(agentsPhrase(shape, pattern, agents)) \(glob ? "suggest" : "suggests")\(later), and edit one before applying it, as you"
        case "webhook":
            return webhookSentence(trigger: pattern, packageName: packageName)
        default:
            return scope
        }
    }

    /// What a `webhook:` scope lets the app do: deliver with its own token,
    /// for any account the package's callable accepts, and read every
    /// delivery to the trigger that could not be filed, whoever sent it.
    /// `packageName` names the package whose trigger it is, where known.
    public static func webhookSentence(trigger: String, packageName: String? = nil) -> String {
        let parked = "and see every delivery to it that could not be filed, whoever sent it"
        guard let packageName else {
            return "Send data to your repository through the \(trigger) webhook, \(parked)"
        }
        return "Send data to your repository through the \(packageName) webhook, for any \(packageName) account, \(parked)"
    }

    /// A scope with `~` in its authority completed with the repository's,
    /// as the server stores it.
    public static func resolve(_ scope: String, authority: String) -> String {
        let (verb, pattern) = split(scope)
        guard !verb.isEmpty, pattern == "~" || pattern.hasPrefix("~/") else { return scope }
        return "\(verb):\(authority)\(pattern.dropFirst())"
    }

    static func split(_ scope: String) -> (verb: String, pattern: String) {
        guard let colon = scope.firstIndex(of: ":") else { return ("", scope) }
        return (String(scope[..<colon]), String(scope[scope.index(after: colon)...]))
    }

    enum Shape {
        case all
        case authority(String)
        case package(authority: String, package: String)
        case exact(authority: String, package: String, name: String)

        init(_ pattern: String) {
            if pattern == "*" {
                self = .all
            } else if pattern.hasSuffix("/*") {
                let parts = pattern.dropLast(2).split(separator: "/", omittingEmptySubsequences: false).map(String.init)
                self = parts.count > 1 && !parts[1].isEmpty ? .package(authority: parts[0], package: parts[1]) : .authority(parts.first ?? "")
            } else {
                let parts = pattern.split(separator: "/", maxSplits: 2, omittingEmptySubsequences: false).map(String.init)
                self = parts.count == 3 ? .exact(authority: parts[0], package: parts[1], name: parts[2])
                    : .exact(authority: "", package: "", name: pattern)
            }
        }
    }

    /// "Apple contacts and events", or "tasks and projects" where the
    /// package's own name would only repeat the first label.
    static func packagePhrase(_ package: String, _ labels: [String]) -> String {
        let name = packageDisplayName(package)
        let lower = name.lowercased()
        let repeats = labels.contains { "\($0.lowercased()) ".hasPrefix("\(lower) ") }
        return repeats ? joinWords(labels) : "\(name) \(joinWords(labels))"
    }

    /// The kinds a scope reaches, grouped by package: the primary kinds by
    /// their labels, the rest counted.
    static func kindsPhrase(_ kinds: [ScopeKind]) -> String {
        var order: [String] = []
        var groups: [String: (package: String, kinds: [ScopeKind])] = [:]
        for kind in kinds {
            let parts = kind.identity.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
            let key = parts.prefix(2).joined(separator: "/")
            if groups[key] == nil {
                order.append(key)
                groups[key] = (parts.count > 1 ? parts[1] : "", [])
            }
            groups[key]!.kinds.append(kind)
        }
        var budget = maxLabels
        var more = 0
        var phrases: [String] = []
        for key in order {
            let group = groups[key]!
            let primary = group.kinds.filter { $0.purpose == nil || $0.purpose == "primary" }
            let named = Array((primary.isEmpty ? group.kinds : primary).prefix(max(budget, 0)))
            more += group.kinds.count - named.count
            budget -= named.count
            guard !named.isEmpty else { continue }
            phrases.append(packagePhrase(group.package, named.map { ($0.plural ?? kindDisplayPlural($0.identity)).lowercasedFirst }))
        }
        let list = phrases.count <= 1 ? (phrases.first ?? "")
            : phrases.dropLast().joined(separator: ", ") + ", and " + phrases.last!
        guard more > 0 else { return list }
        return "\(list), and \(more) more \(more == 1 ? "kind" : "kinds")"
    }

    /// What a kind pattern names when the kinds it reaches are not known.
    static func patternPhrase(_ shape: Shape) -> String {
        switch shape {
        case .all: "records"
        case .authority(let authority): "records from \(authority)"
        case .package(_, let package): "\(packageDisplayName(package)) records"
        case .exact(let authority, let package, let name):
            package.isEmpty ? kindDisplayPlural(name).lowercasedFirst
                : packagePhrase(package, [kindDisplayPlural("\(authority)/\(package)/\(name)").lowercasedFirst])
        }
    }

    static func agentsPhrase(_ shape: Shape, _ pattern: String, _ agents: [String]) -> String {
        if case .all = shape { return "your agents" }
        if !agents.isEmpty { return joinWords(agents) }
        switch shape {
        case .all: return "your agents"
        case .authority(let authority): return "the agents from \(authority)"
        case .package(_, let package): return "the agents in \(packageDisplayName(package))"
        case .exact: return agentWords(pattern)
        }
    }

    /// An agent's id as its name: the words after its last `/` or `:`, else
    /// the whole id (web/console's actor-identity.ts agentName).
    static func agentWords(_ id: String) -> String {
        let local = id.lastIndex(where: { $0 == "/" || $0 == ":" }).map { String(id[id.index(after: $0)...]) } ?? id
        let spaced = local.splittingWords().joined(separator: " ").lowercased()
        return (spaced.isEmpty ? id : spaced).capitalizedFirst
    }
}
