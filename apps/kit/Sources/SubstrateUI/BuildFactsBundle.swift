#if canImport(SwiftUI)
import Foundation
import SubstrateScreens

public extension BuildFacts {
    /// This build as `apple.sh release` stamped it into the bundle, as the
    /// shared updater reads it: a local build is its short version and
    /// build number, on no channel.
    static func bundle(_ bundle: Bundle = .main) -> BuildFacts {
        let info = bundle.infoDictionary ?? [:]
        func value(_ key: String) -> String { (info[key] as? String)?.trimmingCharacters(in: .whitespaces) ?? "" }
        let stamped = value("SubstrateVersion")
        let channel = value("SubstrateChannel")
        // Only an https address is a policy: anything else would be a row
        // that opens nothing.
        let privacy = value("SubstratePrivacyPolicy")
        let policy = URL(string: privacy).flatMap { $0.scheme == "https" && $0.host != nil ? privacy : nil }
        return BuildFacts(
            version: stamped.isEmpty ? "\(value("CFBundleShortVersionString")) (\(value("CFBundleVersion")))" : stamped,
            channel: channel.isEmpty ? nil : channel,
            privacyPolicy: policy)
    }
}
#endif
