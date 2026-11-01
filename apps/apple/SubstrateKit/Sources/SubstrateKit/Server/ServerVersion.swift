import Foundation

/// A substrate build's version as discovery serves it (internal/build): a
/// release (`v0.113.0`), a build past one as `git describe` names it
/// (`v0.113.0-4-gabc1234`, `-dirty` when the tree was), a prerelease or a Go
/// pseudo-version (`v0.114.0-pre.1`, `v0.113.1-0.20261031120000-abc`), which
/// come before the release they name. `dev`, or anything else, is not a
/// version: such a build is not ordered against a release, and only what it
/// can do decides whether an app works with it.
public struct ServerVersion: Comparable, Hashable, Sendable, CustomStringConvertible {
    public var major: Int
    public var minor: Int
    public var patch: Int
    /// What follows a release it comes before: `pre.1`, `rc.2`.
    public var prerelease: String?
    /// How many commits past the tag the build is, from `git describe`.
    public var commitsAfter: Int?

    public init(_ major: Int, _ minor: Int, _ patch: Int, prerelease: String? = nil, commitsAfter: Int? = nil) {
        self.major = major
        self.minor = minor
        self.patch = patch
        self.prerelease = prerelease
        self.commitsAfter = commitsAfter
    }

    public init?(_ text: String) {
        var rest = Substring(text.trimmingCharacters(in: .whitespacesAndNewlines))
        if rest.first == "v" || rest.first == "V" { rest = rest.dropFirst() }
        // Build metadata (`+dirty`, `+incompatible`) never orders.
        if let plus = rest.firstIndex(of: "+") { rest = rest[..<plus] }
        let dash = rest.firstIndex(of: "-")
        let core = rest[..<(dash ?? rest.endIndex)].split(separator: ".", omittingEmptySubsequences: false)
        guard (2 ... 3).contains(core.count) else { return nil }
        let numbers = core.map { part in part.allSatisfy(\.isASCII) ? Int(part) : nil }
        guard numbers.allSatisfy({ $0 != nil && $0! >= 0 }) else { return nil }
        major = numbers[0]!
        minor = numbers[1]!
        patch = numbers.count == 3 ? numbers[2]! : 0
        prerelease = nil
        commitsAfter = nil
        guard let dash else { return }
        let suffix = String(rest[rest.index(after: dash)...])
        guard !suffix.isEmpty else { return nil }
        if let commits = Self.describedCommits(suffix) {
            commitsAfter = commits
        } else {
            prerelease = suffix
        }
    }

    /// `4-gabc1234`, `4-gabc1234-dirty` or `dirty`: commits past the tag.
    private static func describedCommits(_ suffix: String) -> Int? {
        if suffix == "dirty" { return 0 }
        let parts = suffix.split(separator: "-")
        guard parts.count == 2 || (parts.count == 3 && parts[2] == "dirty"),
              let commits = Int(parts[0]), parts[1].hasPrefix("g"), parts[1].count > 1,
              parts[1].dropFirst().allSatisfy(\.isHexDigit)
        else { return nil }
        return commits
    }

    /// A tag's own build: neither before nor past the release it names.
    public var isRelease: Bool { prerelease == nil && commitsAfter == nil }

    /// The release it names or comes before: `0.113.0`.
    public var release: ServerVersion { ServerVersion(major, minor, patch) }

    /// A version as every screen says it: a release as `0.114.0`, a build
    /// before or past one as a build of it, and anything else, `dev`
    /// included, as a development build.
    public static func display(_ text: String?) -> String {
        guard let text, let version = ServerVersion(text) else { return "a development build" }
        return version.isRelease ? version.description : "a build of \(version.release)"
    }

    public var description: String {
        var text = "\(major).\(minor).\(patch)"
        if let prerelease { text += "-\(prerelease)" }
        if let commitsAfter { text += commitsAfter == 0 ? "-dirty" : "-\(commitsAfter)" }
        return text
    }

    public static func < (a: ServerVersion, b: ServerVersion) -> Bool {
        if (a.major, a.minor, a.patch) != (b.major, b.minor, b.patch) {
            return (a.major, a.minor, a.patch) < (b.major, b.minor, b.patch)
        }
        // Before the release, the release, then past it.
        let rank = { (v: ServerVersion) in v.prerelease != nil ? 0 : v.commitsAfter == nil ? 1 : 2 }
        if rank(a) != rank(b) { return rank(a) < rank(b) }
        if let pa = a.prerelease, let pb = b.prerelease { return prereleaseOrder(pa, pb) }
        return (a.commitsAfter ?? 0) < (b.commitsAfter ?? 0)
    }

    /// Semantic versioning's order: dot-separated identifiers, numbers by
    /// value and below words, a shorter list first when one prefixes the
    /// other.
    private static func prereleaseOrder(_ a: String, _ b: String) -> Bool {
        let x = a.split(separator: "."), y = b.split(separator: ".")
        for (p, q) in zip(x, y) where p != q {
            switch (Int(p), Int(q)) {
            case let (m?, n?): return m < n
            case (_?, nil): return true
            case (nil, _?): return false
            default: return p < q
            }
        }
        return x.count < y.count
    }
}
