import Foundation

/// One entry of an app's `authorization_details` (RFC 9396), as
/// internal/substrate's authorize.go carries it: a package the app needs at a
/// version or later, the account it delivers under, or the webhook it
/// delivers to. The token response answers the same shape, with `version`
/// the version now held and `url` where a webhook takes deliveries.
public struct AuthorizationDetail: Codable, Hashable, Sendable {
    /// The three types a substrate sets up.
    public enum Types {
        public static let package = "package"
        public static let account = "account"
        public static let webhook = "webhook"
        public static let all = [package, account, webhook]
    }

    public var type: String
    public var package: String?
    public var version: Int?
    public var kind: String?
    public var id: String?
    public var properties: [String: JSONValue]?
    public var trigger: String?
    public var url: URL?

    public init(type: String, package: String? = nil, version: Int? = nil, kind: String? = nil, id: String? = nil,
                properties: [String: JSONValue]? = nil, trigger: String? = nil, url: URL? = nil)
    {
        self.type = type
        self.package = package
        self.version = version
        self.kind = kind
        self.id = id
        self.properties = properties
        self.trigger = trigger
        self.url = url
    }

    /// A catalog package at `version` or later, installed or upgraded on
    /// approval.
    public static func package(_ id: String, atLeast version: Int) -> AuthorizationDetail {
        AuthorizationDetail(type: Types.package, package: id, version: version)
    }

    /// An account record created once on approval, of a kind a requested
    /// package declares; an existing row, live or deleted, is left alone.
    public static func account(kind: String, id: String, properties: [String: JSONValue] = [:]) -> AuthorizationDetail {
        AuthorizationDetail(type: Types.account, kind: kind, id: id, properties: properties.isEmpty ? nil : properties)
    }

    /// A webhook trigger a requested package ships, turned on on approval;
    /// the approved token carries `webhook:<trigger>` and delivers with it.
    public static func webhook(trigger: String) -> AuthorizationDetail {
        AuthorizationDetail(type: Types.webhook, trigger: trigger)
    }

    enum CodingKeys: String, CodingKey { case type, package, version, kind, id, properties, trigger, url }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = try c.decode(String.self, forKey: .type)
        package = c.value(.package, or: nil)
        version = c.value(.version, or: nil)
        kind = c.value(.kind, or: nil)
        id = c.value(.id, or: nil)
        properties = c.value(.properties, or: nil)
        trigger = c.value(.trigger, or: nil)
        url = c.value(.url, or: nil)
    }

    /// The fields its type carries and nothing else: the server reads a
    /// detail strictly, and an unknown or empty member refuses the request.
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(type, forKey: .type)
        try c.encodeIfPresent(package, forKey: .package)
        if let version, version != 0 { try c.encode(version, forKey: .version) }
        try c.encodeIfPresent(kind, forKey: .kind)
        try c.encodeIfPresent(id, forKey: .id)
        if let properties, !properties.isEmpty { try c.encode(properties, forKey: .properties) }
        try c.encodeIfPresent(trigger, forKey: .trigger)
        try c.encodeIfPresent(url, forKey: .url)
    }

    /// The details as the compact JSON the `authorization_details` parameter
    /// and the check's body carry, keys sorted so one request always reads
    /// the same.
    public static func json(_ details: [AuthorizationDetail]) throws -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return String(decoding: try encoder.encode(details), as: UTF8.self)
    }
}

/// What approving a detail would do, as the server plans it: the action and
/// the facts behind it. Exactly one of `package`, `account` and `webhook`
/// is present, by `type`.
public struct AuthorizationSetup: Codable, Hashable, Sendable {
    /// The actions a row may need. `none` is in place; `unknown` answers a
    /// check for a row the token cannot see, which is never a reason to ask.
    public enum Actions {
        public static let install = "install"
        public static let upgrade = "upgrade"
        public static let create = "create"
        public static let arm = "arm"
        public static let none = "none"
        public static let blocked = "blocked"
        public static let unknown = "unknown"

        /// Whether approving does something for the action.
        public static func needsApproval(_ action: String) -> Bool {
            [install, upgrade, create, arm].contains(action)
        }
    }

    public struct Package: Codable, Hashable, Sendable {
        public var id: String
        public var name: String?
        public var description: String?
        public var required: Int
        /// The catalog release the repository holds, 0 for none.
        public var held: Int
        /// What this server's catalog ships.
        public var shipped: Int

        public init(id: String, name: String? = nil, description: String? = nil, required: Int, held: Int, shipped: Int) {
            self.id = id
            self.name = name
            self.description = description
            self.required = required
            self.held = held
            self.shipped = shipped
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = c.value(.id, or: "")
            name = c.value(.name, or: nil)
            description = c.value(.description, or: nil)
            required = c.value(.required, or: 0)
            held = c.value(.held, or: 0)
            shipped = c.value(.shipped, or: 0)
        }
    }

    public struct Account: Codable, Hashable, Sendable {
        public var kind: String
        public var id: String
        public var properties: [String: JSONValue]?

        public init(kind: String, id: String, properties: [String: JSONValue]? = nil) {
            self.kind = kind
            self.id = id
            self.properties = properties
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            kind = c.value(.kind, or: "")
            id = c.value(.id, or: "")
            properties = c.value(.properties, or: nil)
        }
    }

    public struct Webhook: Codable, Hashable, Sendable {
        public var trigger: String
        public var package: String?
        public var enabled: Bool
        public var keyed: Bool

        public init(trigger: String, package: String? = nil, enabled: Bool = false, keyed: Bool = false) {
            self.trigger = trigger
            self.package = package
            self.enabled = enabled
            self.keyed = keyed
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            trigger = c.value(.trigger, or: "")
            package = c.value(.package, or: nil)
            enabled = c.value(.enabled, or: false)
            keyed = c.value(.keyed, or: false)
        }
    }

    public var type: String
    public var action: String
    /// Why a blocked row is blocked, in the owner's words, and where to go
    /// about it.
    public var problem: String?
    public var fix: String?
    public var package: Package?
    public var account: Account?
    public var webhook: Webhook?

    public init(type: String, action: String, problem: String? = nil, fix: String? = nil, package: Package? = nil,
                account: Account? = nil, webhook: Webhook? = nil)
    {
        self.type = type
        self.action = action
        self.problem = problem
        self.fix = fix
        self.package = package
        self.account = account
        self.webhook = webhook
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        type = c.value(.type, or: "")
        action = c.value(.action, or: Actions.unknown)
        problem = c.value(.problem, or: nil)
        fix = c.value(.fix, or: nil)
        package = c.value(.package, or: nil)
        account = c.value(.account, or: nil)
        webhook = c.value(.webhook, or: nil)
    }
}

/// `POST /api/v1/authorizations/check`'s body: the request the app would
/// send now.
public struct AuthorizationCheck: Codable, Hashable, Sendable {
    public var scope: String
    public var details: [AuthorizationDetail]

    enum CodingKeys: String, CodingKey {
        case scope
        case details = "authorization_details"
    }

    public init(scopes: [String], details: [AuthorizationDetail]) {
        scope = scopes.joined(separator: " ")
        self.details = details
    }
}

/// Whether a token still has what its app needs: the requested scopes it
/// lacks, each detail's action (`unknown` where the token cannot see it) and
/// when the token ends.
public struct AuthorizationCheckResult: Codable, Hashable, Sendable {
    public var missingScopes: [String]
    public var setup: [AuthorizationSetup]
    public var expiresAt: Date?

    enum CodingKeys: String, CodingKey { case scopes, setup, expiresAt }
    enum ScopesKeys: String, CodingKey { case missing }

    public init(missingScopes: [String] = [], setup: [AuthorizationSetup] = [], expiresAt: Date? = nil) {
        self.missingScopes = missingScopes
        self.setup = setup
        self.expiresAt = expiresAt
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let scopes = try? c.nestedContainer(keyedBy: ScopesKeys.self, forKey: .scopes)
        missingScopes = scopes?.value(.missing, or: [String]()) ?? []
        setup = c.value(.setup, or: [])
        expiresAt = (c.value(.expiresAt, or: String?.none)).flatMap(Timestamp.parse)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        var scopes = c.nestedContainer(keyedBy: ScopesKeys.self, forKey: .scopes)
        try scopes.encode(missingScopes, forKey: .missing)
        try c.encode(setup, forKey: .setup)
        try c.encodeIfPresent(expiresAt.map(Timestamp.format), forKey: .expiresAt)
    }
}
