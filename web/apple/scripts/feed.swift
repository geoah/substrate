// The release feeds, from one release's release.json and the index already
// published: index.json (every release, read by the iOS app and by people),
// appcast.xml (Sparkle's, read by the macOS app), index.html (the install
// page) and the release's own manifest.plist (the iOS over-the-air install).
//
//   swift scripts/feed.swift <base-url> <published-index.json|-> <release.json> <out-dir>
//
// Paths inside release.json and index.json are relative to the base URL, so
// the folder can move; the appcast, the manifest and the page need absolute
// addresses and get them here.
import Foundation

struct Artifact: Codable {
    var path: String
    var length: Int
    var sha256: String
    var minimumSystemVersion: String
    /// macOS: the archive's EdDSA signature, which Sparkle refuses to install without.
    var edSignature: String?
    /// iOS: the install manifest and the icons it names.
    var manifest: String?
    var icon: String?
    var smallIcon: String?
}

struct Release: Codable {
    /// The app's name, for the manifest and the page; absent is the console
    /// app, "Substrate", whose feed predates the field.
    var name: String?
    var version: String
    var shortVersion: String
    var build: String
    var channel: String
    var commit: String
    var date: String
    var bundleIdentifier: String
    var macos: Artifact?
    var ios: Artifact?
}

struct Index: Codable {
    var releases: [Release]
}

let args = CommandLine.arguments
guard args.count == 5 else {
    FileHandle.standardError.write(Data("usage: feed.swift <base-url> <published-index.json|-> <release.json> <out-dir>\n".utf8))
    exit(2)
}
let base = URL(string: args[1].hasSuffix("/") ? args[1] : args[1] + "/")!
let out = URL(fileURLWithPath: args[4], isDirectory: true)
let decoder = JSONDecoder()
let release = try decoder.decode(Release.self, from: Data(contentsOf: URL(fileURLWithPath: args[3])))
var index = Index(releases: [])
if args[2] != "-", let data = try? Data(contentsOf: URL(fileURLWithPath: args[2])), !data.isEmpty {
    index = try decoder.decode(Index.self, from: data)
}
index.releases.removeAll { $0.build == release.build }
index.releases.append(release)
index.releases.sort { $0.build.compare($1.build, options: .numeric) == .orderedDescending }
let name = release.name ?? "Substrate"
// Another app's folder holds no Mac build, and its page says so.
let hasMac = index.releases.contains { $0.macos != nil }
let heading = hasMac ? "\(name) for iPhone, iPad and Mac" : "\(name) for iPhone and iPad"
let blurb = name == "Substrate"
    ? "The console in a native app that talks to a substrate's API. Installed builds update themselves from here."
    : "Installed builds offer each newer release themselves."

func absolute(_ path: String) -> String {
    URL(string: path, relativeTo: base)!.absoluteString
}

func xml(_ text: String) -> String {
    text.replacingOccurrences(of: "&", with: "&amp;")
        .replacingOccurrences(of: "<", with: "&lt;")
        .replacingOccurrences(of: ">", with: "&gt;")
        .replacingOccurrences(of: "\"", with: "&quot;")
}

func date(_ iso: String) -> Date {
    ISO8601DateFormatter().date(from: iso) ?? Date()
}

func write(_ text: String, _ name: String) throws {
    try Data(text.utf8).write(to: out.appendingPathComponent(name))
}

// index.json
let encoder = JSONEncoder()
encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
try encoder.encode(index).write(to: out.appendingPathComponent("index.json"))

// appcast.xml: a pre-release rides Sparkle's `prerelease` channel, which a
// build sees only when it includes pre-releases; a stable one has no channel.
let rfc822 = DateFormatter()
rfc822.locale = Locale(identifier: "en_US_POSIX")
rfc822.dateFormat = "EEE, dd MMM yyyy HH:mm:ss Z"
var items = ""
for r in index.releases {
    guard let mac = r.macos, let signature = mac.edSignature else { continue }
    let channel = r.channel == "stable" ? "" : "\n      <sparkle:channel>\(xml(r.channel))</sparkle:channel>"
    items += """

        <item>
          <title>\(xml(r.version))</title>
          <pubDate>\(rfc822.string(from: date(r.date)))</pubDate>
          <sparkle:version>\(xml(r.build))</sparkle:version>
          <sparkle:shortVersionString>\(xml(r.version))</sparkle:shortVersionString>\(channel)
          <sparkle:minimumSystemVersion>\(xml(mac.minimumSystemVersion))</sparkle:minimumSystemVersion>
          <description><![CDATA[<p>Built from commit <code>\(r.commit)</code> on \(r.date).</p>]]></description>
          <enclosure url="\(xml(absolute(mac.path)))" length="\(mac.length)" type="application/octet-stream" sparkle:edSignature="\(xml(signature))"/>
        </item>
    """
}
try write("""
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>\(xml(name))</title>\(items)
  </channel>
</rss>

""", "appcast.xml")

// manifest.plist, for this release alone: iOS fetches it from an
// itms-services link and installs the IPA it names.
if let ios = release.ios, let manifest = ios.manifest {
    var assets: [[String: String]] = [["kind": "software-package", "url": absolute(ios.path)]]
    if let small = ios.smallIcon { assets.append(["kind": "display-image", "url": absolute(small)]) }
    if let icon = ios.icon { assets.append(["kind": "full-size-image", "url": absolute(icon)]) }
    let plist: [String: Any] = [
        "items": [[
            "assets": assets,
            "metadata": [
                "bundle-identifier": release.bundleIdentifier,
                "bundle-version": release.shortVersion,
                "kind": "software",
                "title": name,
            ],
        ]],
    ]
    let file = out.appendingPathComponent(manifest)
    try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
    try PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0).write(to: file)
}

// index.html
func install(_ r: Release) -> String {
    var links: [String] = []
    if let ios = r.ios, let manifest = ios.manifest {
        var allowed = CharacterSet.urlQueryAllowed
        allowed.remove(charactersIn: "&=?+")
        let url = absolute(manifest).addingPercentEncoding(withAllowedCharacters: allowed)!
        links.append("<a href=\"itms-services://?action=download-manifest&amp;url=\(xml(url))\">Install on iPhone or iPad</a>")
    }
    if let mac = r.macos {
        links.append("<a href=\"\(xml(absolute(mac.path)))\">Download for Mac</a>")
    }
    return links.joined(separator: " · ")
}

let display = DateFormatter()
display.locale = Locale(identifier: "en_US_POSIX")
display.timeZone = TimeZone(identifier: "UTC")
display.dateFormat = "yyyy-MM-dd HH:mm 'UTC'"
var rows = ""
for r in index.releases {
    rows += """

          <tr>
            <td><code>\(xml(r.version))</code></td>
            <td>\(r.channel == "stable" ? "Stable" : "Pre-release")</td>
            <td><code>\(xml(r.commit))</code></td>
            <td>\(display.string(from: date(r.date)))</td>
            <td>\(install(r))</td>
          </tr>
    """
}
let mac = "<p><strong>Mac:</strong> download, unzip and move Substrate to Applications before opening it. If macOS says it cannot check the app, open System Settings → Privacy &amp; Security and choose Open Anyway. After that, Substrate → Check for Updates (and the scheduled check) installs newer releases itself.</p>"
let latest = index.releases.first.map { "<p class=\"latest\">\(install($0))</p>" } ?? ""
try write("""
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>\(xml(heading))</title>
  <style>
    :root { color-scheme: light dark; font: 15px/1.5 -apple-system, system-ui, sans-serif; }
    body { max-width: 860px; margin: 2rem auto; padding: 0 1rem; }
    h1 { font-size: 1.5rem; margin-bottom: 0.25rem; }
    h2 { font-size: 1.1rem; margin-top: 2rem; }
    .latest { font-size: 1.1rem; font-weight: 600; }
    table { border-collapse: collapse; width: 100%; font-size: 0.9rem; }
    th, td { text-align: left; padding: 0.4rem 0.6rem 0.4rem 0; border-bottom: 1px solid color-mix(in srgb, currentColor 15%, transparent); vertical-align: top; }
    code { font: 0.85rem ui-monospace, monospace; overflow-wrap: anywhere; }
    .muted { opacity: 0.7; }
  </style>
</head>
<body>
  <h1>\(xml(heading))</h1>
  <p class="muted">\(xml(blurb))</p>
  <h2>Latest</h2>
  \(latest)
  <h2>First install</h2>
  <p><strong>iPhone and iPad:</strong> open this page in Safari and tap Install. Only devices registered to the team can install it, and a development-signed build needs Developer Mode (Settings → Privacy &amp; Security). After that, the app offers each newer release itself.</p>
  \(hasMac ? mac : "")
  <h2>Every release</h2>
  <table>
    <thead><tr><th>Version</th><th>Channel</th><th>Commit</th><th>Built</th><th>Install</th></tr></thead>
    <tbody>\(rows)
    </tbody>
  </table>
</body>
</html>

""", "index.html")
