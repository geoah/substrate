#if canImport(os)
import SubstrateDevice
import os

/// A measurement's intervals and events as `os_signpost`s, which Instruments
/// draws under the subsystem and category given: one lane per name, each
/// interval's message its detail.
public struct SystemSignposts: Signposts {
    private let signposter: OSSignposter

    public init(subsystem: String, category: String = "Measure") {
        signposter = OSSignposter(subsystem: subsystem, category: category)
    }

    public func begin(_ name: StaticString, _ detail: String) -> @Sendable () -> Void {
        let signposter = signposter
        let state = signposter.beginInterval(name, id: signposter.makeSignpostID(), "\(detail, privacy: .public)")
        return { signposter.endInterval(name, state) }
    }

    public func event(_ name: StaticString, _ detail: String) {
        signposter.emitEvent(name, "\(detail, privacy: .public)")
    }
}
#endif
