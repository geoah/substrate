import SubstrateAuth
import SubstrateDeviceApple

extension SessionStore {
    /// The session, either kind, where every build has kept it: readable
    /// after the first unlock so a background refresh and the notification
    /// service extension can use it, and never synced off this device. A
    /// session an earlier build wrote still reads. One definition, compiled
    /// by the app and by the extension, so both read the same item.
    static let chat = SessionStore(
        account: "session",
        storage: KeychainStorage(service: "dev.reamde.substrate.chat", accessibility: .afterFirstUnlockThisDeviceOnly, dataProtection: false))
}
