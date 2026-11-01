import SubstrateKit

extension SessionStore {
    /// The app's Keychain items: the session, either kind, and the install
    /// id beside it, which outlives a sign-out so signing in again keeps
    /// naming the same address book, event store and Health account. The
    /// service and the default access group are the ones every build has
    /// written to, and the share extension reads the session through that
    /// group.
    static let sync = SessionStore(service: "dev.reamde.substrate.sync")
}
