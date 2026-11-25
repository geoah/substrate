import Foundation
import SubstrateCore

// The result shapes the app draws itself, ported from the console's
// web/console/src/lib/tool-views.ts. A function tool's output is matched
// against them in order: a list of web links becomes source pills, a page
// read becomes the page's pill, and the call is said in words. The match is
// on the shape a function returns, never on its name, so the app names no
// provider (decision 0166) and every function that returns a known shape
// draws the same way.
//
// What is read here a web page or a model chose, so nothing is trusted: a
// link is kept only when it is an `http` or `https` URL, and an output of no
// known shape reads as nothing, which leaves the details to show it.

/// One link a search found.
struct WebHit: Hashable, Sendable {
    var title: String
    var url: String
    var snippet: String?
}

/// One page read.
struct WebPage: Hashable, Sendable {
    var url: String
    var title: String?
}

/// A known shape, read.
enum ToolResult: Hashable, Sendable {
    case webHits([WebHit])
    case webPage(WebPage)
}

/// A function tool's settled result in a known shape, read from the
/// `{output, effects}` its function answered; nil while it runs, when it
/// failed, or for an output of no known shape.
func toolResult(_ call: ToolCall) -> ToolResult? {
    guard call.ok == true else { return nil }
    let output = JSONValue.parse(call.output)?["output"]?.object ?? [:]
    return webHits(output) ?? webPage(output)
}

/// A list of links: `results` (or `hits`) holding objects with a `url`.
private func webHits(_ output: [String: JSONValue]) -> ToolResult? {
    guard let list = output["results"]?.array ?? output["hits"]?.array else { return nil }
    let hits = list.compactMap { item -> WebHit? in
        guard let url = webURL(item["url"]?.string) else { return nil }
        return WebHit(title: item["title"]?.text ?? webHost(url), url: url, snippet: item["snippet"]?.text ?? item["description"]?.text)
    }
    // A list whose items are not links is some other list.
    return hits.isEmpty && !list.isEmpty ? nil : .webHits(hits)
}

/// One page: a `url` with the page's `content` or `title` beside it.
private func webPage(_ output: [String: JSONValue]) -> ToolResult? {
    guard let url = webURL(output["url"]?.string), output["content"]?.string != nil || output["title"]?.string != nil else { return nil }
    return .webPage(WebPage(url: url, title: output["title"]?.text))
}

/// A function tool's call in words from the shape of what it returned and
/// what it was asked: "Searched for “…”, 5 results", "Read bbc.co.uk". Nil
/// when neither says anything a reader needs.
func toolResultSummary(_ call: ToolCall) -> String? {
    let query = JSONValue.parse(call.arguments)?["query"]?.text
    switch toolResult(call) {
    case let .webPage(page)?:
        return "Read \(webHost(page.url))"
    case let .webHits(hits)?:
        let found = hits.count == 1 ? "1 result" : "\(hits.count) results"
        return query.map { "Searched for “\($0)”, \(found)" } ?? "Found \(found)"
    case nil:
        return call.ok == false ? nil : query.map { "Searched for “\($0)”" }
    }
}

/// The address a link shows: its host, without a leading `www.`.
func webHost(_ url: String) -> String {
    guard let components = URLComponents(string: url), let host = components.host?.lowercased(), !host.isEmpty else { return url }
    let port = components.port.map { ":\($0)" } ?? ""
    return (host.hasPrefix("www.") ? String(host.dropFirst(4)) : host) + port
}

/// A value as a web address the app may link: an absolute `http` or `https`
/// URL with a host, its scheme and host in lower case, else nil.
func webURL(_ value: String?) -> String? {
    guard let raw = value?.trimmingCharacters(in: .whitespacesAndNewlines), var components = URLComponents(string: raw),
        let scheme = components.scheme?.lowercased(), scheme == "http" || scheme == "https",
        let host = components.host, !host.isEmpty
    else { return nil }
    components.scheme = scheme
    components.host = host.lowercased()
    if components.path.isEmpty { components.path = "/" }
    return components.string
}
