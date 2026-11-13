import Foundation
import SubstrateCore
import SubstrateDevice

/// `GET /.well-known/substrate/server.json`, all of it (internal/api's
/// discovery.go). Every field is read tolerantly: a server older or newer
/// than this kit omits or adds fields, and a missing one takes the value an
/// app would assume without asking.
public struct Discovery: Codable, Hashable, Sendable {
    /// A served API prefix and the status it is served under.
    public struct APIVersion: Codable, Hashable, Sendable {
        public var name: String
        /// `served` or `deprecated`.
        public var status: String

        public init(name: String, status: String) {
            self.name = name
            self.status = status
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            name = c.value(.name, or: "")
            status = c.value(.status, or: "")
        }
    }

    /// The running build: the release it was cut from (`v0.113.0`), a build
    /// past one (`v0.113.0-4-gabc1234`) or `dev`, and its commit.
    public struct Server: Codable, Hashable, Sendable {
        public var version: String?
        public var build: String?

        public init(version: String?, build: String? = nil) {
            self.version = version
            self.build = build
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            version = c.value(.version, or: nil)
            build = c.value(.build, or: nil)
        }
    }

    /// The oldest resumable changelog seq; 0 while nothing is pruned.
    public struct Changelog: Codable, Hashable, Sendable {
        public var horizon: Int

        public init(horizon: Int) { self.horizon = horizon }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            horizon = c.value(.horizon, or: 0)
        }
    }

    /// One feature the deployment serves: a group of routes, the promise
    /// their shape is served under, how far the group has grown and the
    /// surfaces that serve it.
    public struct Feature: Codable, Hashable, Sendable {
        public var name: String
        /// `stable` (only additions within 1.x), `beta` (supported; a break
        /// is announced one minor release ahead, in `notice`) or
        /// `experimental` (may change or go in any release). Kept as text:
        /// the set is the server's, and a label this kit has not met is no
        /// reason to refuse the document.
        public var stability: String
        /// Starts at 1 and only rises, each time the feature gains something
        /// a client may check for. 0 is a server from before revisions,
        /// which meets no minimum a client asks for.
        public var revision: Int
        /// A coming break or a deprecation, where the server announces one.
        public var notice: String?
        public var surfaces: [String]

        public init(name: String, stability: String, revision: Int = 1, surfaces: [String] = ["rest"], notice: String? = nil) {
            self.name = name
            self.stability = stability
            self.revision = revision
            self.surfaces = surfaces
            self.notice = notice
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            name = c.value(.name, or: "")
            stability = c.value(.stability, or: "")
            revision = max(0, c.value(.revision, or: 0))
            notice = c.value(.notice, or: nil)
            surfaces = c.value(.surfaces, or: [])
        }
    }

    public struct Surface: Codable, Hashable, Sendable {
        public var endpoint: String
        public var compatibility: String

        public init(endpoint: String, compatibility: String) {
            self.endpoint = endpoint
            self.compatibility = compatibility
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            endpoint = c.value(.endpoint, or: "")
            compatibility = c.value(.compatibility, or: "")
        }
    }

    public struct Surfaces: Codable, Hashable, Sendable {
        public var rest: Surface?

        public init(rest: Surface?) { self.rest = rest }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            rest = c.value(.rest, or: nil)
        }
    }

    /// How the deployment spells a kind and a record reference.
    public struct Grammar: Codable, Hashable, Sendable {
        public var kind: String
        public var record: String
        public var collection: String
        public var recordPath: String
        /// The types of actor record a write links (decision 0270).
        public var actorTypes: [String]

        public init(kind: String = "", record: String = "", collection: String = "", recordPath: String = "", actorTypes: [String] = []) {
            self.kind = kind
            self.record = record
            self.collection = collection
            self.recordPath = recordPath
            self.actorTypes = actorTypes
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            kind = c.value(.kind, or: "")
            record = c.value(.record, or: "")
            collection = c.value(.collection, or: "")
            recordPath = c.value(.recordPath, or: "")
            actorTypes = c.value(.actorTypes, or: [])
        }
    }

    /// The doors beside the versioned API. `authorize` and `token` are the
    /// app authorization flow's, absent on a server that predates it.
    public struct Endpoints: Codable, Hashable, Sendable {
        public var register: String?
        public var login: String?
        public var tokens: String?
        public var password: String?
        public var totp: String?
        public var authorize: String?
        public var token: String?

        public init(
            register: String? = "/register", login: String? = "/login", tokens: String? = "/tokens",
            password: String? = "/password", totp: String? = "/totp",
            authorize: String? = nil, token: String? = nil
        ) {
            self.register = register
            self.login = login
            self.tokens = tokens
            self.password = password
            self.totp = totp
            self.authorize = authorize
            self.token = token
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            register = c.value(.register, or: nil)
            login = c.value(.login, or: nil)
            tokens = c.value(.tokens, or: nil)
            password = c.value(.password, or: nil)
            totp = c.value(.totp, or: nil)
            authorize = c.value(.authorize, or: nil)
            token = c.value(.token, or: nil)
        }
    }

    /// What the register and login doors ask for. Absent reads as asked:
    /// a form that offers a field the server ignores costs a person less
    /// than one that hides a field the server wants.
    public struct Registration: Codable, Hashable, Sendable {
        public var inviteRequired: Bool
        public var totpRequired: Bool

        public init(inviteRequired: Bool = true, totpRequired: Bool = true) {
            self.inviteRequired = inviteRequired
            self.totpRequired = totpRequired
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            inviteRequired = c.value(.inviteRequired, or: true)
            totpRequired = c.value(.totpRequired, or: true)
        }
    }

    public var versions: [APIVersion]
    public var server: Server
    public var changelog: Changelog
    public var features: [Feature]
    public var surfaces: Surfaces
    public var grammar: Grammar
    public var endpoints: Endpoints
    public var registration: Registration

    public init(
        versions: [APIVersion] = [APIVersion(name: "v1", status: "served")], server: Server,
        changelog: Changelog = Changelog(horizon: 0), features: [Feature] = [],
        surfaces: Surfaces = Surfaces(rest: Surface(endpoint: "/api/v1", compatibility: "supported")),
        grammar: Grammar = Grammar(), endpoints: Endpoints = Endpoints(), registration: Registration = Registration()
    ) {
        self.versions = versions
        self.server = server
        self.changelog = changelog
        self.features = features
        self.surfaces = surfaces
        self.grammar = grammar
        self.endpoints = endpoints
        self.registration = registration
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        versions = c.value(.versions, or: [])
        server = c.value(.server, or: Server(version: nil))
        changelog = c.value(.changelog, or: Changelog(horizon: 0))
        features = c.value(.features, or: [])
        surfaces = c.value(.surfaces, or: Surfaces(rest: nil))
        grammar = c.value(.grammar, or: Grammar())
        endpoints = c.value(.endpoints, or: Endpoints(register: nil, login: nil, tokens: nil, password: nil, totp: nil))
        registration = c.value(.registration, or: Registration())
    }

    /// The running build's version, where it names one this kit can order.
    public var version: ServerVersion? { server.version.flatMap(ServerVersion.init) }

    public func feature(_ name: String) -> Feature? { features.first { $0.name == name } }

    /// Whether the server serves a feature, at `revision` or a later one
    /// where a minimum is asked. The server refuses a request field it does
    /// not know, so a client asks this before it sends one a revision added.
    public func serves(_ name: String, atLeast revision: Int = 0) -> Bool {
        feature(name).map { $0.revision >= revision } ?? false
    }

    /// Whether the server says it serves app authorization (decision 0194).
    /// Its OAuth metadata is the authority on what that covers.
    public var servesApps: Bool { feature("apps") != nil || endpoints.authorize != nil }

    /// Whether the server's door names a person (`auth` at revision 3,
    /// decision 0274): a sign-in sends `identity`, the name the person signs
    /// in with. An older door takes that name as `repository` and refuses
    /// `identity`, and every person there signs in with their repository's
    /// name.
    public var doorNamesAPerson: Bool { serves("auth", atLeast: 3) }
}
