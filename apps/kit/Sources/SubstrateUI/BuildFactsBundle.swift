#if canImport(SwiftUI)
import Foundation
import SubstrateScreens

public extension BuildFacts {
    /// This build as `apple.sh release` stamped it into the bundle
    /// (`BuildFacts.stamped`): a local build, and one Apple updates, is its
    /// short version and build number, on no channel.
    static func bundle(_ bundle: Bundle = .main) -> BuildFacts { stamped(bundle.infoDictionary ?? [:]) }
}
#endif
