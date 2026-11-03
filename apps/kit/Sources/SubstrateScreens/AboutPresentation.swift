/// The build, its channel and an update when one is offered (§5.6's About).
public struct AboutPresentation: Codable, Hashable, Sendable {
    public var title: String
    public var rows: [LabeledValue]
    public var update: UpdateOffer?
    /// "Check for Updates", where the build has a feed.
    public var check: String?

    public init(title: String, rows: [LabeledValue], update: UpdateOffer?, check: String?) {
        self.title = title
        self.rows = rows
        self.update = update
        self.check = check
    }

    /// - Parameters:
    ///   - version: the build's version, a pre-release's in full.
    ///   - channel: the feed the build follows, nil for a local build.
    ///   - offered: a newer build's version, while one is offered.
    public static func present(appName: String, version: String, channel: String?, offered: String?) -> AboutPresentation {
        var rows = [LabeledValue("Version", version, verbatim: true)]
        if let channel { rows.append(LabeledValue("Channel", channel == "pre" ? "Pre-release" : channel.capitalizedFirst)) }
        return AboutPresentation(
            title: "About", rows: rows,
            update: offered.map { UpdateOffer(title: "Update available", message: "\(appName) \($0) is ready to install. You have \(version).",
                                              install: "Install", later: "Not Now") },
            check: channel == nil ? nil : "Check for Updates")
    }
}

/// A newer build, offered.
public struct UpdateOffer: Codable, Hashable, Sendable {
    public var title: String
    public var message: String
    public var install: String
    public var later: String

    public init(title: String, message: String, install: String, later: String) {
        self.title = title
        self.message = message
        self.install = install
        self.later = later
    }
}
