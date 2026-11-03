import SubstrateCore

/// The build, its channel and an update when one is offered (§5.6's About).
public struct AboutPresentation: Bridged, Hashable, Sendable {
    public var title: String
    public var version: LabeledValue
    /// The feed the build follows; nil for a local build.
    public var channel: LabeledValue?
    public var update: UpdateOffer?
    /// "Check for Updates", where the build has a feed.
    public var check: String?

    public init(title: String, version: LabeledValue, channel: LabeledValue?, update: UpdateOffer?, check: String?) {
        self.title = title
        self.version = version
        self.channel = channel
        self.update = update
        self.check = check
    }

    /// The version, and the channel where there is one.
    public var rows: [LabeledValue] { [version] + (channel.map { [$0] } ?? []) }

    /// - Parameters:
    ///   - version: the build's version, a pre-release's in full.
    ///   - channel: the feed the build follows, nil for a local build.
    ///   - offered: a newer build's version, while one is offered.
    public static func present(appName: String, version: String, channel: String?, offered: String?) -> AboutPresentation {
        AboutPresentation(
            title: "About", version: LabeledValue("Version", version),
            channel: channel.map { LabeledValue("Channel", $0 == "pre" ? "Pre-release" : $0.capitalizedFirst) },
            update: offered.map { UpdateOffer(title: "Update available", message: "\(appName) \($0) is ready to install. You have \(version).",
                                              install: "Install", later: "Not Now") },
            check: channel == nil ? nil : "Check for Updates")
    }
}

/// A newer build, offered.
public struct UpdateOffer: Bridged, Hashable, Sendable {
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
