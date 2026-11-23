import Foundation
import SubstrateCore

/// Where a record can be read in full at the place it came from: a message
/// in Slack, a mail in Gmail, an issue in Linear. A task says what it was
/// made from (`origin`); this is that record's own page.
public struct SourcePage: Codable, Equatable, Hashable, Sendable {
    /// The place, as a button names it: "Slack", "Gmail", "Linear".
    public var name: String
    public var url: String

    public init(name: String, url: String) {
        self.name = name
        self.url = url
    }
}

/// What a button that leaves the app opens: a record's page at its source,
/// or the record itself in the server's console.
public struct SourceLink: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var id: String
    /// The word on the button: "Slack", "Record".
    public var title: String
    /// What it does, said in full: "Open in Slack".
    public var label: String
    public var url: String

    public init(id: String, title: String, label: String, url: String) {
        self.id = id
        self.title = title
        self.label = label
        self.url = url
    }
}

/// How a record's page at its source is found. No kind declares which of
/// its addresses is that page, so it is found by the names the providers
/// use, and a Gmail message's is composed as the provider itself does.
public enum SourcePages {
    /// The properties a record names its own page in, in the order they are
    /// tried: GitHub's `url` is its API's, so `htmlUrl` is first.
    static let properties = ["permalink", "htmlUrl", "htmlLink", "webViewLink", "externalUrl", "url", "publicUrl"]

    /// The records a record's page is composed from, where it names none:
    /// a Gmail message's thread and account.
    public static func related(_ record: Record) -> [String] {
        guard named(record) == nil, isMail(record.kind) else { return [] }
        return [record["thread"]?.referencePath, record["account"]?.referencePath].compactMap { $0 }
    }

    /// The record's page at its source, or nil where it has none: a chat
    /// that has no address, a record nobody mirrored from anywhere.
    public static func page(of record: Record, with related: [String: Record] = [:]) -> SourcePage? {
        if let url = named(record) { return SourcePage(name: name(of: url), url: url.absoluteString) }
        guard isMail(record.kind) else { return nil }
        // Gmail opens a conversation at `/mail/u/<account>/#all/<thread>`. Without the account's
        // address the link would open in whichever account the browser has first, and find nothing.
        let account = record["account"]?.referencePath.flatMap { related[$0] }
        guard let address = account.flatMap(address), let who = address.addingPercentEncoding(withAllowedCharacters: mailAccount) else { return nil }
        let thread = record["thread"]?.referencePath.flatMap { related[$0] }?["threadId"]?.text ?? record["threadId"]?.text ?? record["messageId"]?.text
        guard let thread, !thread.isEmpty, thread.allSatisfy({ $0.isLetter || $0.isNumber }) else { return nil }
        return SourcePage(name: "Gmail", url: "https://mail.google.com/mail/u/\(who)/#all/\(thread)")
    }

    /// The address a record names as its own page: the first of
    /// `properties` that holds a web address which is not an API's.
    static func named(_ record: Record) -> URL? {
        for property in properties {
            guard let text = record[property]?.text, let url = URL(string: text), let scheme = url.scheme?.lowercased(), let host = url.host else {
                continue
            }
            guard scheme == "https" || scheme == "http", !host.lowercased().hasPrefix("api.") else { continue }
            return url
        }
        return nil
    }

    static func isMail(_ kind: String) -> Bool { kind.hasSuffix("/google/gmailmessage") || kind.hasSuffix("/google/gmailthread") }

    private static func address(_ account: Record) -> String? {
        let text = account["email"]?.text ?? account["email"]?["value"]?.text ?? account["email"]?["address"]?.text
        return text.flatMap { $0.contains("@") ? $0 : nil }
    }

    private static let mailAccount = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~@")

    /// What a place is called, from its address: the names of the places
    /// the providers mirror, else the host itself.
    public static func name(of url: URL) -> String {
        let host = (url.host ?? "").lowercased()
        func under(_ domain: String) -> Bool { host == domain || host.hasSuffix("." + domain) }
        if under("slack.com") { return "Slack" }
        if host == "mail.google.com" { return "Gmail" }
        if host == "docs.google.com" { return "Google Docs" }
        if host == "drive.google.com" { return "Google Drive" }
        if host == "meet.google.com" { return "Google Meet" }
        if host == "calendar.google.com" || (under("google.com") && url.path.hasPrefix("/calendar")) { return "Google Calendar" }
        if under("linear.app") { return "Linear" }
        if under("github.com") { return "GitHub" }
        if under("notion.so") || under("notion.site") { return "Notion" }
        return host.hasPrefix("www.") ? String(host.dropFirst(4)) : host
    }

    /// The record's own page in the server's console (`/data/<kind>/<id>`),
    /// which a browser opens: everything the record holds.
    public static func console(_ server: URL?, path: String) -> String? {
        guard let server, let reference = RecordReference(path: path) else { return nil }
        let id = reference.recordID.addingPercentEncoding(withAllowedCharacters: unreserved) ?? reference.recordID
        return ServerAddress.page(server, path: "/data/\(reference.kind)/\(id)")?.absoluteString
    }

    private static let unreserved = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~")

    /// The buttons for something made from a record: its page at the
    /// source, where one is known, then the record itself.
    public static func links(for path: String?, page: SourcePage?, server: URL?) -> [SourceLink] {
        guard let path else { return [] }
        var out: [SourceLink] = []
        if let page { out.append(SourceLink(id: "source", title: page.name, label: "Open in \(page.name)", url: page.url)) }
        if let console = console(server, path: path) {
            out.append(SourceLink(id: "record", title: "Record", label: "Open the record in Substrate", url: console))
        }
        return out
    }
}
