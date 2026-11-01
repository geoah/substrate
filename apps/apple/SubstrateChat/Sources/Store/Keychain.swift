import SubstrateKit

extension SessionStore {
    /// The session, either kind, where every build has kept it: readable
    /// after the first unlock so a background refresh can use it, and never
    /// synced off this device. A session an earlier build wrote still reads.
    static let chat = SessionStore(service: "dev.reamde.substrate.chat")
}
