import SubstrateAPI
import SubstrateAuth
import SubstrateCore

/// A server that cannot do what the app needs: which release it runs, which
/// the app needs, what it cannot do yet, and who can change that.
public struct ServerTooOldPresentation: Bridged, Hashable, Sendable {
    public var symbol: ScreenSymbol
    public var title: String
    public var summary: String
    /// "It can't yet", over what follows; nil when nothing does.
    public var cannotTitle: String?
    public var missing: [ChangeLine]
    public var note: String
    public var tryAgain: String?
    public var changeServer: String?

    public init(symbol: ScreenSymbol, title: String, summary: String, cannotTitle: String?, missing: [ChangeLine], note: String,
                tryAgain: String?, changeServer: String?)
    {
        self.symbol = symbol
        self.title = title
        self.summary = summary
        self.cannotTitle = cannotTitle
        self.missing = missing
        self.note = note
        self.tryAgain = tryAgain
        self.changeServer = changeServer
    }

    /// - Parameters:
    ///   - blocked: the package rows only a newer server sets up, each with
    ///     the server's sentence.
    ///   - actions: whether the screen offers Try Again and Use Another
    ///     Server.
    public static func present(appName: String, host: String, running: String?, needed: String?, missing: [String],
                               blocked: [Change] = [], tryAgain: Bool, changeServer: Bool) -> ServerTooOldPresentation
    {
        let release = running.flatMap(ServerVersion.init)?.isRelease == true
        let runs = "\(host) runs \(ServerVersion.display(running))" + (release ? "." : " that can't do it yet.")
        let summary = needed.map { "\(appName) needs Substrate \($0) or later. \(runs)" } ?? "\(appName) needs a newer Substrate. \(runs)"
        let lines = missing.map { ChangeLine(id: "missing|\($0)", symbol: .cannot, tone: .quiet, words: $0.capitalizedFirst) }
            + blocked.map(ChangeLine.init)
        return ServerTooOldPresentation(symbol: .serverUpdate, title: "\(host) needs an update", summary: summary,
                                        cannotTitle: lines.isEmpty ? nil : "It can't yet", missing: lines,
                                        note: "Whoever runs \(host) can update it. Once it is, try again.",
                                        tryAgain: tryAgain ? "Try Again" : nil, changeServer: changeServer ? "Use Another Server" : nil)
    }
}
