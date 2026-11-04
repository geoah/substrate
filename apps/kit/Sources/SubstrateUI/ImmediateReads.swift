#if canImport(Security)
import Foundation
import Security
import SubstrateAuth
import SubstrateDevice

/// The session store's reads on the calling thread, for an Apple app's code
/// that cannot suspend (a view, a lock's critical section): the Keychain
/// answers there (`ImmediateSessionStorage`). Each answers as its awaited
/// twin does. Over a storage that answers elsewhere, which no Apple app
/// keeps, each reads as a store that could not answer, never waiting.
public extension SessionStore {
    func read() -> Read {
        Read(data(account: account))
    }

    func load() -> Session? {
        if case .found(let session) = read() { return session }
        return nil
    }

    func pendingRevocations() throws -> [Session] {
        try Self.revocations(in: data(account: revocationsAccount))
    }

    func data(account: String) -> Item {
        guard let immediate = storage as? any ImmediateSessionStorage else { return .failed(errSecNotAvailable) }
        return immediate.read(account: account)
    }

    func string(account: String) -> String? {
        if case .found(let data) = data(account: account) { return String(data: data, encoding: .utf8) }
        return nil
    }

    func setString(_ value: String, account: String) throws {
        guard let immediate = storage as? any ImmediateSessionStorage else { throw SecureStoreRefused(status: errSecNotAvailable) }
        try immediate.write(Data(value.utf8), account: account)
    }
}
#endif
