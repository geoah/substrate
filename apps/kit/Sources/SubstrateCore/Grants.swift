import Foundation

/// What an app token may do, from its scopes, matched as the server matches
/// them (internal/scope): `read:` and `write:` over kinds, or over the kinds
/// that bind a trait (`read:implements(<trait>)`, decision 0225), `write`
/// including read; `agents:` and `decide:` over agent ids; `call:` over
/// function ids; `webhook:` one trigger. Neither a glob nor a trait scope
/// reaches an internal kind, no scope reaches the auth kinds, none writes
/// core or llm, and no trait scope writes an account. `full` (decision 0204)
/// is all of it: the server minted a token without scopes.
public struct Grants: Codable, Hashable, Sendable {
    public var scopes: [String]

    public init(_ scopes: [String]) {
        self.scopes = scopes
    }

    /// Whether the owner approved full access.
    public var full: Bool { scopes.contains(ScopeWords.full) }

    /// The three core kinds that hold the repository's own auth material.
    static let authKinds = Set(["token", "credential", "secret"].map { "substrate.reamde.dev/core/\($0)" })
    /// The packages no scope writes.
    static let unwritable = ["substrate.reamde.dev/core/", "substrate.reamde.dev/llm/"]
    /// The seeded packages whose every kind is internal, for a caller that
    /// does not know a kind's declared purpose.
    static let machinery = ["substrate.reamde.dev/core/", "substrate.reamde.dev/llm/", "substrate.reamde.dev/console/"]
    /// The trait an account binds: no trait scope writes one, whatever else
    /// it binds.
    static let accountConfig = "substrate.reamde.dev/core/accountconfig"

    /// Whether the grant reads records of `kind`.
    public func reads(_ kind: GrantKind) -> Bool {
        if full { return true }
        return parsed.contains { ($0.verb == "read" || $0.verb == "write") && Self.matches($0.pattern, kind) }
    }

    /// Whether the grant writes records of `kind`.
    public func writes(_ kind: GrantKind) -> Bool {
        if full { return true }
        guard !Self.unwritable.contains(where: kind.reference.hasPrefix) else { return false }
        return parsed.contains { scope in
            guard scope.verb == "write" else { return false }
            if ScopeWords.trait(of: scope.pattern) != nil, kind.binds(Self.accountConfig) { return false }
            return Self.matches(scope.pattern, kind)
        }
    }

    /// Whether the grant reads records of the kind `kind` names, knowing no
    /// trait it binds, so no trait scope reaches it. `purpose` is its
    /// declared purpose where the caller read it.
    public func reads(_ kind: String, purpose: String? = nil) -> Bool { reads(GrantKind(kind, purpose: purpose)) }

    /// Whether the grant writes records of the kind `kind` names, knowing no
    /// trait it binds, so no trait scope reaches it.
    public func writes(_ kind: String, purpose: String? = nil) -> Bool { writes(GrantKind(kind, purpose: purpose)) }

    /// Whether the grant talks to the agent whose record id is `agent`.
    public func talksTo(_ agent: String) -> Bool { full || matchesAgent("agents", agent) }

    /// Whether the grant decides what the agent whose record id is `agent`
    /// proposes.
    public func decides(_ agent: String) -> Bool { full || matchesAgent("decide", agent) }

    /// Whether the grant calls the function whose record id is `function`
    /// (decision 0219). The function also reaches only what the grant's
    /// reads and writes cover, which the server holds and this does not.
    public func calls(_ function: String) -> Bool { full || matchesAgent("call", function) }

    /// Whether the grant delivers to the webhook trigger `trigger` and reads
    /// its parked deliveries. Never under `full`: the webhook door reads a
    /// full-access bearer as the trigger's key, which it is not.
    public func delivers(_ trigger: String) -> Bool {
        !trigger.isEmpty && parsed.contains { $0.verb == "webhook" && $0.pattern == trigger }
    }

    /// Whether the grant already holds what `scope` asks for, as the server
    /// decides it (internal/scope's Covers): a scope of the same verb, or a
    /// write for a read, over a pattern at least as wide. `internal` is
    /// whether the scope names one kind whose declared purpose is `internal`,
    /// which a glob never reaches, so only the exact scope covers it. A trait
    /// scope reaches what `*` does among the kinds that bind the trait, so
    /// only `*` covers it, and since which kinds bind a trait moves with the
    /// registry, it covers no scope but itself. A webhook scope covers its
    /// own trigger alone. `full` covers everything.
    public func covers(_ scope: String, internal internalKind: Bool = false) -> Bool {
        if full { return true }
        let want = ScopeWords.split(scope)
        return parsed.contains { held in
            guard held.verb == want.verb || (held.verb == "write" && want.verb == "read") else { return false }
            if held.pattern == want.pattern { return true }
            if Self.isTraitScope(want) { return held.pattern == "*" }
            if Self.isTraitScope(held) { return false }
            if want.verb == "webhook" || !Self.isGlob(held.pattern) { return false }
            if Self.isGlob(want.pattern) { return Self.globPrefix(want.pattern).hasPrefix(Self.globPrefix(held.pattern)) }
            if internalKind, want.verb == "read" || want.verb == "write" { return false }
            return Self.matchesGlob(held.pattern, want.pattern)
        }
    }

    /// What every name a glob matches starts with: nothing for `*`, and the
    /// pattern up to its `*` otherwise.
    static func globPrefix(_ glob: String) -> String { glob.hasSuffix("*") ? String(glob.dropLast()) : glob }

    /// Whether the grant holds any `agents:` scope.
    public var talks: Bool { full || parsed.contains { $0.verb == "agents" } }

    /// Whether the grant holds any `decide:` scope.
    public var decidesAny: Bool { full || parsed.contains { $0.verb == "decide" } }

    /// The approved access as the console's everyday sentences, one a scope.
    public var words: [String] { scopes.map { ScopeWords.sentence($0) } }

    var parsed: [(verb: String, pattern: String)] { scopes.map(ScopeWords.split) }

    private func matchesAgent(_ verb: String, _ agent: String) -> Bool {
        !agent.isEmpty && parsed.contains { $0.verb == verb && Self.matchesGlob($0.pattern, agent) }
    }

    static func isInternal(_ kind: String, purpose: String?) -> Bool {
        purpose.map { $0 == "internal" } ?? machinery.contains(where: kind.hasPrefix)
    }

    /// One pattern against one kind: the auth kinds are never reached, and
    /// neither a glob nor a trait scope reaches an internal kind.
    static func matches(_ pattern: String, _ kind: GrantKind) -> Bool {
        if authKinds.contains(kind.reference) { return false }
        if let trait = ScopeWords.trait(of: pattern) { return !kind.isInternal && kind.binds(trait) }
        if isGlob(pattern) { return !kind.isInternal && matchesGlob(pattern, kind.reference) }
        return pattern == kind.reference
    }

    /// Whether a scope is a read or write scope over a trait.
    static func isTraitScope(_ scope: (verb: String, pattern: String)) -> Bool {
        (scope.verb == "read" || scope.verb == "write") && ScopeWords.trait(of: scope.pattern) != nil
    }

    static func isGlob(_ pattern: String) -> Bool { pattern == "*" || pattern.hasSuffix("/*") }

    /// `*`, a prefix up to its trailing `/`, or one exact name.
    static func matchesGlob(_ pattern: String, _ name: String) -> Bool {
        if pattern == "*" { return true }
        if pattern.hasSuffix("/*") { return name.hasPrefix(pattern.dropLast()) }
        return pattern == name
    }
}

/// One kind as a read or write scope is held to it (internal/scope's
/// `Kind`): its reference, its declared purpose, and the traits its
/// declaration binds, each by its full identity. A kind whose declaration
/// the caller has not read binds no trait here, so no trait scope reaches it:
/// the server, which reads the declaration, stays the judge.
public struct GrantKind: Hashable, Sendable {
    public var reference: String
    /// The declared purpose; nil where the caller did not read it, which
    /// counts a kind of the seeded core, llm and console packages as
    /// internal, as every one of them is.
    public var purpose: String?
    public var traits: [String]

    public init(_ reference: String, purpose: String? = nil, traits: [String] = []) {
        self.reference = reference
        self.purpose = purpose
        self.traits = traits
    }

    /// The kind as its declaration says it, a purpose left out read as
    /// `primary`.
    public init(_ declaration: KindDeclaration) {
        self.init(declaration.reference, purpose: declaration.purpose ?? "primary", traits: declaration.traits)
    }

    var isInternal: Bool { Grants.isInternal(reference, purpose: purpose) }

    /// Whether the kind binds `trait`, matched in its canonical spelling
    /// alone.
    func binds(_ trait: String) -> Bool { traits.contains(trait) }
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
/// app may do, over the kinds, agents or functions it reaches, by their
/// labels. Every power stays in the words: a write may delete, a read
/// searches by meaning at the owner's cost, a glob or a trait reaches what is
/// added later, talking to an agent reaches whatever the agent may do and
/// reads every chat with it, a decision may edit the change it applies, and
/// a call runs the owner's code as far as the app already reaches.
public enum ScopeWords {
    /// How many kind labels one sentence names before it counts the rest.
    static let maxLabels = 6

    /// What every read or write scope adds (decision 0219): a semantic
    /// search over the kinds it reads embeds the query with the owner's
    /// provider.
    static let semanticClause = ", and search them by meaning, which spends your embedding budget"

    /// The sentence for one scope. `kinds` are the kinds it reaches today,
    /// where the caller knows them; without them a pattern reads by its
    /// shape, and a trait scope says nothing of which kinds have the trait,
    /// since an empty list says none does. `agents` are the names of the
    /// agents an agent scope matches.
    public static func sentence(
        _ scope: String, kinds: [ScopeKind]? = nil, agents: [String] = [],
        functions: [String] = [], packageName: String? = nil
    ) -> String {
        let (verb, pattern) = split(scope)
        let glob = Grants.isGlob(pattern)
        let shape = Shape(pattern)
        switch verb {
        case "read", "write":
            let action = verb == "read" ? "See" : "See, add, change and delete"
            if let trait = trait(of: pattern) {
                let today = kinds.map { $0.isEmpty ? " (no kind has it yet)" : " (today your \(kindsPhrase($0)))" } ?? ""
                return "\(action) your records of every kind with the \(traitWord(trait)) trait\(today), including kinds that take it on later\(semanticClause)"
            }
            let later = glob ? ", including kinds added later" : ""
            if case .all = shape { return "\(action) all your records\(later)\(semanticClause)" }
            let what = kinds.flatMap { $0.isEmpty ? nil : kindsPhrase($0) } ?? patternPhrase(shape)
            return "\(action) your \(what)\(later)\(semanticClause)"
        case "call":
            let later = glob ? ", including functions added later" : ""
            let who = functionsPhrase(shape, pattern, functions)
            return "Run \(who)\(later); \(glob ? "each runs" : "it runs") only where the app may already see and change what it reads and writes"
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
            return
                "Apply or dismiss the changes \(agentsPhrase(shape, pattern, agents)) \(glob ? "suggest" : "suggests")\(later), and edit one before applying it, as you"
        case "webhook":
            return webhookSentence(trigger: pattern, packageName: packageName)
        case full:
            return fullSentence
        default:
            return scope
        }
    }

    /// The one scope with no pattern, asked for alone.
    public static let full = "full"

    /// What `full` lets an app do, as the approval page says it.
    public static let fullSentence =
        "Do everything you can do when you sign in: see, add, change and delete any record, "
        + "change your kinds, settings and agents, run your functions, and make or revoke tokens, including this one"

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

    /// A scope naming the repository's own kinds by its full name,
    /// `authority`, a trait scope's trait's included, with the name folded
    /// into `~`, the one spelling a token stores (decision record 0259):
    /// `read:ada.example.com/tasks/*` is `read:~/tasks/*` in the repository
    /// named ada.example.com. A scope naming another authority, or `~`
    /// already, comes back unchanged.
    public static func localize(_ scope: String, authority: String) -> String {
        let (verb, pattern) = split(scope)
        guard !verb.isEmpty, !pattern.isEmpty, !authority.isEmpty else { return scope }
        if verb == "read" || verb == "write", let trait = trait(of: pattern) {
            return "\(verb):\(traitOpen)\(localize(reference: trait, authority: authority))\(traitClose)"
        }
        return "\(verb):\(localize(reference: pattern, authority: authority))"
    }

    /// A kind, package or glob reference under `authority` moved under `~`
    /// (`vocabulary.LocalizeRef`).
    static func localize(reference: String, authority: String) -> String {
        guard reference.hasPrefix(authority + "/") else { return reference }
        return KindReference.local + reference.dropFirst(authority.count)
    }

    /// How a read or write pattern names a trait rather than kinds, as the
    /// records filter spells one (decision 0225).
    static let traitOpen = "implements("
    static let traitClose = ")"

    /// The trait a read or write pattern names, `implements(<trait>)`, or nil
    /// for a kind pattern.
    public static func trait(of pattern: String) -> String? {
        guard pattern.count > traitOpen.count + traitClose.count, pattern.hasPrefix(traitOpen), pattern.hasSuffix(traitClose)
        else { return nil }
        return String(pattern.dropFirst(traitOpen.count).dropLast(traitClose.count))
    }

    /// A trait by its name, as a sentence says it: `lifecycle`, and
    /// `due date` for `dueDate`.
    static func traitWord(_ trait: String) -> String {
        let parts = trait.split(separator: "/", maxSplits: 2, omittingEmptySubsequences: false)
        let name = parts.count == 3 ? parts[2] : Substring(trait)
        var words = ""
        var previous: Character?
        for character in name {
            if let previous, previous.isASCII, previous.isLowercase || previous.isNumber, character.isASCII, character.isUppercase {
                words.append(" ")
            }
            words.append(character)
            previous = character
        }
        return words.lowercased()
    }

    public static func split(_ scope: String) -> (verb: String, pattern: String) {
        if scope == full { return (full, "") }
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
                self =
                    parts.count == 3
                    ? .exact(authority: parts[0], package: parts[1], name: parts[2])
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
        let list =
            phrases.count <= 1
            ? (phrases.first ?? "")
            : phrases.dropLast().joined(separator: ", ") + ", and " + phrases.last!
        guard more > 0 else { return list }
        return "\(list), and \(more) more \(more == 1 ? "kind" : "kinds")"
    }

    /// What a kind pattern names when the kinds it reaches are not known.
    static func patternPhrase(_ shape: Shape) -> String {
        switch shape {
        case .all: "records"
        case .authority(KindReference.local): "records of the kinds you made or added"
        case .authority(let authority): "records from \(authority)"
        case .package(_, let package): "\(packageDisplayName(package)) records"
        case .exact(let authority, let package, let name):
            package.isEmpty
                ? kindDisplayPlural(name).lowercasedFirst
                : packagePhrase(package, [kindDisplayPlural("\(authority)/\(package)/\(name)").lowercasedFirst])
        }
    }

    /// The functions a call scope names (decision 0219): by name where the
    /// caller knows which it matches, else by what its pattern covers.
    static func functionsPhrase(_ shape: Shape, _ pattern: String, _ functions: [String]) -> String {
        if case .all = shape { return "any of your functions" }
        if !functions.isEmpty { return joinWords(functions) }
        switch shape {
        case .all: return "any of your functions"
        case .authority(KindReference.local): return "the functions you made or added"
        case .authority(let authority): return "the functions from \(authority)"
        case .package(_, let package): return "the functions in \(packageDisplayName(package))"
        case .exact: return agentWords(pattern)
        }
    }

    static func agentsPhrase(_ shape: Shape, _ pattern: String, _ agents: [String]) -> String {
        if case .all = shape { return "your agents" }
        if !agents.isEmpty { return joinWords(agents) }
        switch shape {
        case .all: return "your agents"
        case .authority(KindReference.local): return "the agents you made or added"
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
