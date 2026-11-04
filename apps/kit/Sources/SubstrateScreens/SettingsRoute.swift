import SubstrateRuntime

/// Where a settings row's command goes: the app's root, the link, or the UI,
/// which runs what only it can (the browser for Change Access, the updater,
/// the connect screen).
public enum SettingsRoute<App: LinkedApp>: Equatable, Sendable {
    case app(App.Command)
    case link(LinkController<App>.Command)
    /// The review of the app's access, through the browser.
    case changeAccess
    /// The updater's own check.
    case checkForUpdates
    /// The privacy policy the build names, in the browser.
    case privacyPolicy
    /// The connect screen.
    case signIn
    case none

    /// - Parameters:
    ///   - settings: the app's command for one of its own rows' toggles and
    ///     choices.
    ///   - clearSaved: the app's command for Storage's Clear Saved Copies,
    ///     nil where it keeps none.
    public static func route(_ command: SettingsCommand, settings: (SettingsCommand) -> App.Command?, clearSaved: App.Command?)
        -> SettingsRoute
    {
        switch command {
        case .toggle(.app, _), .choose(.app, _), .open(.app), .perform(.app): settings(command).map(SettingsRoute.app) ?? .none
        case .perform(.storage(.clear)): clearSaved.map(SettingsRoute.app) ?? .none
        case .perform(.status(.action(.sendNow))): .link(.sendNow)
        case .perform(.status(.action(.readAgain))): .link(.readAgain)
        case let .perform(.status(.action(.retry(id)))): .link(.retry(id))
        case let .perform(.status(.action(.discard(id)))): .link(.discard(id))
        case .perform(.status(.action(.review))), .perform(.status(.action(.renew))), .perform(.repository(.changeAccess)): .changeAccess
        case .perform(.repository(.signOut)): .link(.signOut)
        case .perform(.status(.action(.signIn))): .signIn
        case .perform(.about(.check)): .checkForUpdates
        case .perform(.about(.privacyPolicy)): .privacyPolicy
        default: .none
        }
    }
}
