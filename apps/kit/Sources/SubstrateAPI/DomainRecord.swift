import Foundation
import SubstrateCore
import SubstrateDevice

/// The `_substrate` TXT record a domain publishes about itself: where the
/// repository of that name is served (`server=`), the code that proved it
/// (`proof=`), and where its authority document is (`doc=`, pinned by
/// `sha256=`). Read by the same rules as the server's
/// `internal/domains/record.go`, so the app and the server never disagree on
/// what one record says.
public struct DomainRecord: Hashable, Sendable {
    /// The DNS label the record lives under: `_substrate.<name>`.
    public static let label = "_substrate"
    /// What opens every record read here. A TXT string that does not start
    /// with it belongs to something else at the same label and is skipped.
    public static let version = "v=substrate1"

    /// The origin (`https://host[:port]`) of the substrate holding the
    /// repository of this name.
    public var server: String?
    public var proof: String?
    /// The URL of the authority document this name publishes.
    public var doc: String?
    /// Lower-case hex of the document's SHA-256.
    public var sha256: String?
    /// Every key this kit does not read, kept so a later key never refuses
    /// the record.
    public var other: [String: String] = [:]

    public init(server: String? = nil, proof: String? = nil, doc: String? = nil, sha256: String? = nil,
                other: [String: String] = [:])
    {
        self.server = server
        self.proof = proof
        self.doc = doc
        self.sha256 = sha256
        self.other = other
    }

    /// A record that exists and cannot be read: no proof of anything, with
    /// words a person can be shown.
    public struct Malformed: LocalizedError, Hashable, Sendable {
        public let reason: String
        public var errorDescription: String? { "The _substrate record is malformed: \(reason)." }
    }

    /// The name looked up for `name`: `_substrate.<name>`, lower-cased and
    /// without a trailing dot. Nil for anything that is not a domain name of
    /// two labels or more, so nothing asks the resolver for a URL or a
    /// single label.
    public static func queryName(for name: String) -> String? {
        var host = name.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        if host.hasSuffix(".") { host.removeLast() }
        guard !host.isEmpty, host.count <= 253, host.contains("."),
              host.rangeOfCharacter(from: CharacterSet(charactersIn: "/:@ ")) == nil
        else { return nil }
        return "\(label).\(host)"
    }

    /// Reads the TXT strings answered at one `_substrate` label: nil when
    /// none opens with `version`, `Malformed` when more than one does (a name
    /// that says two things says nothing) or the one that does cannot be
    /// read. Each string is one TXT record, its character-strings joined.
    public static func parse(_ txts: [String]) throws -> DomainRecord? {
        let ours = txts.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { $0 == version || $0.hasPrefix(version + " ") }
        guard let text = ours.first else { return nil }
        guard ours.count == 1 else {
            throw Malformed(reason: "\(ours.count) records open with \(version), and a name may carry one")
        }
        var record = DomainRecord()
        var seen: Set<String> = []
        for field in text.dropFirst(version.count).split(whereSeparator: \.isWhitespace) {
            guard let eq = field.firstIndex(of: "=") else { throw Malformed(reason: "\(field) is not key=value") }
            let key = String(field[..<eq])
            let value = String(field[field.index(after: eq)...])
            guard !key.isEmpty, !value.isEmpty else { throw Malformed(reason: "\(field) is not key=value") }
            guard seen.insert(key).inserted else { throw Malformed(reason: "\(key) appears twice") }
            switch key {
            case "server":
                guard let origin = httpsOrigin(value) else {
                    throw Malformed(reason: "server=\(value) is not an https origin without a path, query, fragment or user")
                }
                record.server = origin
            case "proof":
                record.proof = value
            case "doc":
                guard isHTTPSURL(value) else { throw Malformed(reason: "doc=\(value) is not an https URL without a user or fragment") }
                record.doc = value
            case "sha256":
                guard value.count == 64, value.allSatisfy({ $0.isASCII && $0.isHexDigit }) else {
                    throw Malformed(reason: "sha256=\(value) is not 64 hex digits")
                }
                record.sha256 = value.lowercased()
            default:
                record.other[key] = value
            }
        }
        if record.sha256 != nil, record.doc == nil {
            throw Malformed(reason: "sha256= pins a document and the record names none with doc=")
        }
        return record
    }

    /// The origin a `server=` value names, `https://host[:port]` lower-cased
    /// without a trailing slash, or nil when it carries more than an origin.
    static func httpsOrigin(_ value: String) -> String? {
        guard let parts = URLComponents(string: value), parts.scheme?.lowercased() == "https",
              let host = parts.host, !host.isEmpty,
              parts.user == nil, parts.password == nil,
              (parts.query ?? "").isEmpty, (parts.fragment ?? "").isEmpty,
              parts.path.isEmpty || parts.path == "/"
        else { return nil }
        let bracketed = host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
        return "https://" + bracketed.lowercased() + (parts.port.map { ":\($0)" } ?? "")
    }

    static func isHTTPSURL(_ value: String) -> Bool {
        guard let parts = URLComponents(string: value), parts.scheme?.lowercased() == "https",
              let host = parts.host, !host.isEmpty
        else { return false }
        return parts.user == nil && parts.password == nil && (parts.fragment ?? "").isEmpty
    }
}
