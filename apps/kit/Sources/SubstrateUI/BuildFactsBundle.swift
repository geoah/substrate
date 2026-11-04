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
        return BuildFacts(
            version: stamped.isEmpty ? "\(value("CFBundleShortVersionString")) (\(value("CFBundleVersion")))" : stamped,
            channel: channel.isEmpty ? nil : channel)
    }
}
#endif
