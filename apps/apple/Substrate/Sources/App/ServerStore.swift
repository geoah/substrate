import CryptoKit
import Foundation
import SubstrateKit
import WebKit

/// The substrate the app is pointed at, and the ones it was pointed at before.
/// A server is its origin alone (`https://host[:port]`): the console asks for
/// paths, and the shell puts them under this origin.
@MainActor
final class ServerStore: ObservableObject {
    private static let currentKey = "server.current"
    private static let recentKey = "server.recent"

    @Published private(set) var current: URL?
    @Published private(set) var recent: [URL]

    init(defaults: UserDefaults = .standard) {
        current = defaults.string(forKey: Self.currentKey).flatMap(ServerAddress.origin(of:))
        recent = (defaults.stringArray(forKey: Self.recentKey) ?? []).compactMap(ServerAddress.origin(of:))
        #if DEBUG
        // DebugDriver's runs start pointed at a server, past the picker.
        if let preset = ProcessInfo.processInfo.environment["SUBSTRATE_SHELL_SERVER"].flatMap(ServerAddress.origin(of:)) {
            current = preset
        }
        #endif
    }

    func use(_ server: URL) {
        current = server
        recent = [server] + recent.filter { $0 != server }.prefix(7)
        UserDefaults.standard.set(server.absoluteString, forKey: Self.currentKey)
        UserDefaults.standard.set(recent.map(\.absoluteString), forKey: Self.recentKey)
    }

    func forget(_ server: URL) {
        recent.removeAll { $0 == server }
        UserDefaults.standard.set(recent.map(\.absoluteString), forKey: Self.recentKey)
        WKWebsiteDataStore.remove(forIdentifier: Self.storeIdentifier(for: server)) { _ in }
    }

    /// Each server keeps its own website data: its token, its console
    /// preferences. Pointing the app elsewhere and back finds the session
    /// where it was left, and a token never reaches a server it was not
    /// minted by.
    static func dataStore(for server: URL) -> WKWebsiteDataStore {
        WKWebsiteDataStore(forIdentifier: storeIdentifier(for: server))
    }

    private static func storeIdentifier(for server: URL) -> UUID {
        var bytes = Array(SHA256.hash(data: Data(server.absoluteString.utf8)).prefix(16))
        bytes[6] = (bytes[6] & 0x0F) | 0x50 // a name-based UUID
        bytes[8] = (bytes[8] & 0x3F) | 0x80
        return UUID(uuid: (bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7],
                           bytes[8], bytes[9], bytes[10], bytes[11], bytes[12], bytes[13], bytes[14], bytes[15]))
    }
}
