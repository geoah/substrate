import SubstrateCore

/// A mark a screen shows beside its words, by what it means; each system
/// draws it as its own symbol (an SF Symbol, a Material Symbol).
public enum ScreenSymbol: String, Bridged, Hashable, Sendable, CaseIterable {
    case serverUpdate
    case warning
    case approval
    case clock
    case ended
    case offline
    case key
    case server
    case found
    case accessEnded
    case allowed
    case leftOut
    case install
    case upgrade
    case create
    case webhook
    case cannot
    case upToDate
    case sending
    case device
    case problem
}
