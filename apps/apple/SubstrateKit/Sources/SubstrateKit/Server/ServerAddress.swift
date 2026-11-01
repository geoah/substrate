import Foundation

/// A server as a person types it, made the one origin every app stores and
/// builds its requests on: `https://host[:port]`, nothing after it.
public enum ServerAddress {
    /// The origin a typed address names. A bare host is HTTPS, except a
    /// loopback one (`localhost`, `127.x.x.x`, `[::1]`), which is what
    /// `mise run dev` serves over plain HTTP. The host is lowercased and the
    /// port kept; a path, query, user or fragment is dropped, since the
    /// server's paths are the client's to add and two spellings of one
    /// server must bind the same session. Nil for anything but http or https.
    public static func origin(of typed: String) -> URL? {
        var text = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return nil }
        if !text.contains("://") {
            // Without a scheme, an `@` before the path is an email address or
            // another scheme's (`mailto:`), never a server.
            let authority = text.prefix { !"/?#".contains($0) }
            guard !authority.contains("@") else { return nil }
            text = (isLoopback(host: host(in: authority)) ? "http://" : "https://") + text
        }
        guard let parts = URLComponents(string: text),
              let scheme = parts.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = parts.host, !host.isEmpty
        else { return nil }
        var origin = URLComponents()
        origin.scheme = scheme
        origin.host = host.lowercased()
        origin.port = parts.port
        return origin.url
    }

    /// Whether a server is on this machine: its address reaches nobody else,
    /// and nothing proves who answers on it.
    public static func isLoopback(_ server: URL) -> Bool {
        isLoopback(host: server.host(percentEncoded: false)?.lowercased() ?? "")
    }

    /// The host a person reads: `ada.example.com`, `localhost:8080`.
    public static func display(_ server: URL) -> String {
        guard let host = server.host(percentEncoded: false), !host.isEmpty else { return server.absoluteString }
        let bracketed = host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
        return server.port.map { "\(bracketed):\($0)" } ?? bracketed
    }

    private static func isLoopback(host: String) -> Bool {
        if host == "localhost" || host == "[::1]" || host == "::1" { return true }
        let octets = host.split(separator: ".", omittingEmptySubsequences: false)
        return octets.count == 4 && octets.first == "127" && octets.allSatisfy { !$0.isEmpty && $0.allSatisfy(\.isASCII) && UInt8($0) != nil }
    }

    /// The host of an authority typed without a scheme, lowercased, without
    /// the port.
    private static func host(in authority: Substring) -> String {
        if authority.hasPrefix("["), let close = authority.firstIndex(of: "]") {
            return String(authority[...close]).lowercased()
        }
        return String(authority.split(separator: ":", maxSplits: 1).first ?? "").lowercased()
    }
}
