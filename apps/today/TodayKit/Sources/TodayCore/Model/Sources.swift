import Foundation
import SubstrateCore

/// Where a record can be read in full at the place it came from: a message
/// in Slack, a mail in Gmail, an issue in Linear. A task says what it was
/// made from (`origin`); this is that record's own page.
public struct SourcePage: Codable, Equatable, Hashable, Sendable {
    /// The place, as a button names it: "Slack", "Gmail", "Linear".
    public var name: String
    public var url: String
    /// What the source is inside its place: a Slack message's channel as
    /// "#name", or "Direct message", "Group message". Nil where a place has
    /// no such part, and in what an earlier build kept.
    public var group: String?

    public init(name: String, url: String, group: String? = nil) {
        self.name = name
        self.url = url
        self.group = group
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

    /// The records read beside a record for its page: a Gmail message's
    /// thread and account, which its page is composed from where it names
    /// none, and a Slack message's conversation, which holds the channel's
    /// name. A direct message says what it is itself.
    public static func related(_ record: Record) -> [String] {
        if isSlackMessage(record.kind) {
            guard directGroup(record["channelType"]?.text) == nil else { return [] }
            return [record["channel"]?.referencePath].compactMap { $0 }
        }
        guard named(record) == nil, isMail(record.kind) else { return [] }
        return [record["thread"]?.referencePath, record["account"]?.referencePath].compactMap { $0 }
    }

    /// The record's page at its source, or nil where it has none: a chat
    /// that has no address, a record nobody mirrored from anywhere.
    public static func page(of record: Record, with related: [String: Record] = [:]) -> SourcePage? {
        if let url = named(record) {
            return SourcePage(name: name(of: url), url: url.absoluteString, group: isSlackMessage(record.kind) ? slackGroup(record, with: related) : nil)
        }
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

    static func isSlackMessage(_ kind: String) -> Bool { kind.hasSuffix("/slack/message") }

    /// Whether a record's page says what it is inside its place: a Slack
    /// message's does, so one kept without it is read once more.
    static func namesGroup(_ path: String) -> Bool { RecordReference(path: path).map { isSlackMessage($0.kind) } ?? false }

    /// A conversation that is no channel, by Slack's own word for its type.
    private static func directGroup(_ type: String?) -> String? {
        switch type?.lowercased() {
        case "im": "Direct message"
        case "mpim": "Group message"
        default: nil
        }
    }

    /// What a Slack message's conversation is called: a channel by its
    /// name, "#name"; a direct message and a group one by what they are,
    /// since Slack gives the first no name and the second a made-up one. A
    /// channel whose name is not known has none.
    static func slackGroup(_ record: Record, with related: [String: Record]) -> String? {
        let conversation = record["channel"]?.referencePath.flatMap { related[$0] }
        if let direct = directGroup(record["channelType"]?.text) ?? directGroup(conversation?["channelType"]?.text) { return direct }
        if conversation?["isIm"]?.bool == true { return "Direct message" }
        if conversation?["isMpim"]?.bool == true { return "Group message" }
        guard let name = conversation?["name"]?.text?.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty else { return nil }
        return name.hasPrefix("#") ? name : "#" + name
    }

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

    /// The place a record is mirrored from, known from its kind alone, so
    /// before anything is read: Slack, Gmail, Google Calendar, Linear,
    /// GitHub, Notion. Nil for a kind that says no one place: a file in a
    /// drive is a document or a sheet, and its address says which.
    public static func place(ofKind kind: String) -> String? {
        let parts = kind.split(separator: "/")
        guard parts.count >= 2 else { return nil }
        let name = parts[parts.count - 1]
        switch parts[parts.count - 2] {
        case "slack", "slacklive": return "Slack"
        case "linear": return "Linear"
        case "github": return "GitHub"
        case "notion": return "Notion"
        case "google":
            if name.hasPrefix("gmail") || name.hasSuffix("mail") { return "Gmail" }
            if name.hasPrefix("calendar") { return "Google Calendar" }
            return nil
        default: return nil
        }
    }

    /// Where something written from a record came from, as a line says it:
    /// the place, from the record's kind or else from its page once read,
    /// then what it is inside the place once that is read. "Slack",
    /// "Slack, #api-reliability", "Slack, direct message", "Gmail".
    public static func label(for path: String?, page: SourcePage?) -> String? {
        guard let path, let place = RecordReference(path: path).flatMap({ place(ofKind: $0.kind) }) ?? page?.name else { return nil }
        guard let group = page?.group, !group.isEmpty else { return place }
        return "\(place), \(group.hasPrefix("#") ? group : Words.low(group))"
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
    /// - Parameter placeSaid: the place is said in words right above the
    ///   buttons (a section's "Slack, #api-reliability", a line's "Gmail."):
    ///   the button then says "Open" and does not name the place a second
    ///   time. What it is told aloud stays "Open in Slack".
    public static func links(for path: String?, page: SourcePage?, server: URL?, placeSaid: Bool = false) -> [SourceLink] {
        guard let path else { return [] }
        var out: [SourceLink] = []
        if let page {
            out.append(SourceLink(id: "source", title: placeSaid ? "Open" : page.name, label: "Open in \(page.name)", url: page.url))
        }
        if let console = console(server, path: path) {
            out.append(SourceLink(id: "record", title: "Record", label: "Open the record in Substrate", url: console))
        }
        return out
    }
}
