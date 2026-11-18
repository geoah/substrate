import Foundation
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// The publisher's push relay as an install speaks to it (version 1 of its
/// API): one registration, sent by the app itself with no substrate token
/// and following no redirect, so neither the owner's server nor its history
/// ever sees Apple's token. A relay is named by its origin and optional path
/// (`push.relay` in the publisher's authority document); every address is
/// built from that the same way the server and the relay build it.
public enum PushRelay {
    public static let registerPath = "/v1/register"
    public static let pushPath = "/v1/push"
    /// How long a relay's `push.relay` value may be.
    public static let maxRelay = 2048
    /// How long a push key the relay seals may be.
    public static let maxPushKey = 1024

    /// The address of `path` under `relay`, the `push.relay` an authority
    /// document names: the value as written, less one trailing `/` of its
    /// path, then `path`. Nothing else is normalised, so the server, the
    /// app and the publisher's tooling compare the same strings
    /// (`pushwire.Endpoint`, whose vectors the kit's tests read). `relay` is
    /// an `https` URL with a host and a port, if any, from 1 to 65535, an
    /// optional path, no user info, query or fragment, and only printable
    /// ASCII, at most 2,048 characters. Nil for anything else.
    public static func endpoint(_ relay: String, path: String) -> String? {
        let scheme = "https://"
        guard path.hasPrefix("/"), relay.utf8.count <= maxRelay, relay.hasPrefix(scheme),
            relay.utf8.allSatisfy({ $0 > 0x20 && $0 < 0x7F && $0 != UInt8(ascii: "?") && $0 != UInt8(ascii: "#") && $0 != UInt8(ascii: "\\") })
        else { return nil }
        let authority = relay.dropFirst(scheme.count).prefix { $0 != "/" }
        guard !authority.isEmpty, !authority.contains("@") else { return nil }
        // A `%` starts two hex digits, and in the host only a byte past
        // ASCII may be written so, as Go's URL parser holds it.
        guard validEscapes(relay.utf8, host: false), validEscapes(authority.utf8, host: true) else { return nil }
        var host = Substring(authority)
        var port: Substring?
        if authority.hasPrefix("[") {
            guard let close = authority.firstIndex(of: "]") else { return nil }
            host = authority[authority.index(after: authority.startIndex) ..< close]
            let after = authority[authority.index(after: close)...]
            if !after.isEmpty {
                guard after.hasPrefix(":") else { return nil }
                port = after.dropFirst()
            }
        } else if let colon = authority.firstIndex(of: ":") {
            host = authority[..<colon]
            port = authority[authority.index(after: colon)...]
        }
        guard !host.isEmpty else { return nil }
        if let port {
            guard !port.isEmpty, port.allSatisfy(\.isASCII), port.allSatisfy(\.isNumber), let number = Int(port), (1 ... 65535).contains(number)
            else { return nil }
        }
        return (relay.hasSuffix("/") ? String(relay.dropLast()) : relay) + path
    }

    private static func validEscapes(_ bytes: some Collection<UInt8>, host: Bool) -> Bool {
        let bytes = Array(bytes)
        var index = 0
        while index < bytes.count {
            defer { index += 1 }
            guard bytes[index] == UInt8(ascii: "%") else { continue }
            guard index + 2 < bytes.count, let high = hex(bytes[index + 1]), hex(bytes[index + 2]) != nil, !host || high >= 8 else { return false }
            index += 2
        }
        return true
    }

    private static func hex(_ byte: UInt8) -> UInt8? {
        switch byte {
        case UInt8(ascii: "0") ... UInt8(ascii: "9"): byte - UInt8(ascii: "0")
        case UInt8(ascii: "a") ... UInt8(ascii: "f"): byte - UInt8(ascii: "a") + 10
        case UInt8(ascii: "A") ... UInt8(ascii: "F"): byte - UInt8(ascii: "A") + 10
        default: nil
        }
    }

    /// Whether two relays are the same one: both valid, and the same
    /// address for a push.
    public static func same(_ a: String, _ b: String) -> Bool {
        guard let first = endpoint(a, path: pushPath), let second = endpoint(b, path: pushPath) else { return false }
        return first == second
    }

    /// `POST /v1/register`'s body.
    public struct RegisterRequest: Codable, Equatable, Sendable {
        public var platform: String
        public var token: String
        public var topic: String
        public var environment: String
        public var publicKey: String
        public var generation: String

        public init(platform: String, token: String, topic: String, environment: String, publicKey: String, generation: String) {
            self.platform = platform
            self.token = token
            self.topic = topic
            self.environment = environment
            self.publicKey = publicKey
            self.generation = generation
        }
    }

    public struct RegisterResponse: Codable, Equatable, Sendable {
        public var pushKey: String
    }

    /// The relay's refusal, by the code from its closed set; its message is
    /// for a log, never for a person.
    public struct Refusal: LocalizedError, Equatable, Sendable {
        public var status: Int
        public var code: String
        public var message: String

        public init(status: Int, code: String, message: String) {
            self.status = status
            self.code = code
            self.message = message
        }

        public var errorDescription: String? { "The push relay refused (\(status) \(code))." }

        /// A refusal the install can do nothing about by trying again: the
        /// relay serves no such app or environment.
        public var stops: Bool { code == "topic_refused" || code == "environment_refused" }

        static func of(status: Int, data: Data) -> Refusal? {
            guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any], let error = object["error"] as? [String: Any],
                let code = error["code"] as? String
            else { return nil }
            return Refusal(status: status, code: code, message: (error["message"] as? String) ?? "")
        }
    }

    /// Registers an install with the relay and answers the push key it
    /// sealed. The request carries no token and a redirect is refused, so it
    /// reaches the relay the build names and nothing else.
    public static func register(_ request: RegisterRequest, relay: String, transport: any HTTPTransport) async throws -> String {
        guard let address = endpoint(relay, path: registerPath), let url = URL(string: address) else { throw URLError(.badURL) }
        var http = URLRequest(url: url)
        http.httpMethod = "POST"
        http.timeoutInterval = 15
        http.setValue("application/json", forHTTPHeaderField: "Content-Type")
        http.setValue("application/json", forHTTPHeaderField: "Accept")
        http.httpBody = try JSONEncoder().encode(request)
        let (data, response) = try await transport.data(for: http)
        if (300 ..< 400).contains(response.statusCode) {
            throw Redirected(status: response.statusCode, location: response.value(forHTTPHeaderField: "Location"))
        }
        guard response.statusCode == 200 else {
            throw Refusal.of(status: response.statusCode, data: data) ?? TransportError(status: response.statusCode, body: data)
        }
        let answer = try JSONDecoder().decode(RegisterResponse.self, from: data)
        guard !answer.pushKey.isEmpty, answer.pushKey.count <= maxPushKey else { throw URLError(.cannotParseResponse) }
        return answer.pushKey
    }

    /// The relay the publisher's authority document names for one app: the
    /// document at its site, the origin of the app's own client id, at
    /// `/.well-known/substrate/authority.json`, and in it the entry whose
    /// `client_id` is the app's own, its `push.relay`. Read leniently, as the
    /// server reads it: a `push` that is not an object, a `relay` that is not
    /// a string, or one that is no relay address, names none. Nil where the
    /// document names none, or there is no document (`404`); throws where it
    /// could not be read now, which says nothing either way.
    public static func published(clientID: String, transport: any HTTPTransport) async throws -> String? {
        guard let client = URLComponents(string: clientID), client.scheme == "https", let host = client.host, !host.isEmpty else {
            return nil
        }
        var site = URLComponents()
        site.scheme = "https"
        site.host = host
        site.port = client.port
        site.path = "/.well-known/substrate/authority.json"
        guard let url = site.url else { return nil }
        var http = URLRequest(url: url)
        http.timeoutInterval = 15
        http.setValue("application/json", forHTTPHeaderField: "Accept")
        let (data, response) = try await transport.data(for: http)
        if response.statusCode == 404 { return nil }
        if (300 ..< 400).contains(response.statusCode) {
            throw Redirected(status: response.statusCode, location: response.value(forHTTPHeaderField: "Location"))
        }
        guard response.statusCode == 200 else { throw TransportError(status: response.statusCode, body: data) }
        guard let document = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw URLError(.cannotParseResponse) }
        let apps = document["apps"] as? [Any] ?? []
        for case let app as [String: Any] in apps where app["client_id"] as? String == clientID {
            guard let push = app["push"] as? [String: Any], let relay = push["relay"] as? String,
                endpoint(relay, path: pushPath) != nil
            else { return nil }
            return relay
        }
        return nil
    }
}

/// `GET /api/v1/push/key`: the public half of the key the server signs this
/// token's pushes with, and the relay it sends them to, which the app holds
/// to its build and its publisher's document and never sends anything to.
public struct PushKey: Codable, Equatable, Sendable {
    public var publicKey: String
    public var relay: String

    public init(publicKey: String, relay: String) {
        self.publicKey = publicKey
        self.relay = relay
    }
}

/// `GET /api/v1/push/badge`.
public struct PushBadge: Codable, Equatable, Sendable {
    public var badge: Int
}

/// Why the server pushes nothing to this token.
public enum PushRefused: String, Error, Sendable, Equatable {
    /// The token is no verified app's, its app is local, or its publisher's
    /// document names no relay for it.
    case noRelay = "no_relay"
    /// The operator turned push off.
    case off = "push_off"
}

public extension SubstrateClient {
    /// The kind of the record one install that takes pushes keeps, named by
    /// the id of the token it signed in with.
    static let deviceKind = "substrate.reamde.dev/core/device"

    /// The push key and relay for this token, or why there is none. Throws
    /// where the server could not say now (`503` while it cannot read the
    /// publisher's document), which says nothing either way.
    func pushKey() async throws -> Result<PushKey, PushRefused> {
        do {
            return .success(try await send("GET", "api/v1/push/key", timeout: Timeout.quick, retries: 0).decode(PushKey.self))
        } catch let error as ServerError where error.status == 403 {
            if let refused = error.reason.flatMap(PushRefused.init(rawValue:)) { return .failure(refused) }
            throw error
        }
    }

    /// The person's unread count as the server counts it for this token:
    /// what a push sets the badge to. Nil where the operator turned push
    /// off, when the app counts for itself.
    func pushBadge() async throws -> Int? {
        do {
            let badge = try await send("GET", "api/v1/push/badge", timeout: Timeout.quick).decode(PushBadge.self).badge
            return min(max(badge, 0), 999)
        } catch let error as ServerError where error.status == 403 && error.reason == PushRefused.off.rawValue {
            return nil
        }
    }

    /// Writes this install's device record at its token's id: a put that
    /// merges, answered `201` the first time and `200` after. `token` and
    /// `person` are the server's to fill.
    func putDevice(tokenID: String, name: String, platform: String, pushKey: String, notifies: [String]) async throws {
        let properties: [String: JSONValue] = [
            "name": .string(String(name.prefix(100))), "platform": .string(platform), "pushKey": .string(pushKey),
            "notifies": .array(notifies.map(JSONValue.string)),
        ]
        try await put(Self.deviceKind, tokenID, properties: properties)
    }
}
