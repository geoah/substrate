import Foundation
import Security
import TodayGlance

/// The widgets' day as one Keychain item, which the app writes and its
/// widget extension reads: what the two share with no capability to
/// register, as Substrate Chat shares its session with its notification
/// extension. Compiled into the app's UI framework and into the extension,
/// so both name the same item.
///
/// No access group is named here. The app and the extension each list the
/// app's own identifier as their one Keychain group
/// (`keychain-access-groups` in their entitlements, the team's prefix
/// before `dev.reamde.substrate.today`), so that is the group a write goes
/// to and the group a read searches.
enum GlanceKeychain {
    static let service = "dev.reamde.substrate.today.glance"
    static let account = "day"

    /// What names the item: one generic password under the service and the
    /// account, never synchronised to iCloud.
    static var item: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecAttrSynchronizable as String: false,
        ]
    }

    /// What a write sets: the value, readable once the device has been
    /// unlocked a first time, so a widget drawn on a locked screen reads it.
    static func attributes(_ data: Data) -> [String: Any] {
        [
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
            kSecValueData as String: data,
        ]
    }

    /// Replaces the item's value in place, or adds the item where there is
    /// none. False when the Keychain refused, which leaves what was there.
    @discardableResult static func write(_ data: Data) -> Bool {
        var status = SecItemUpdate(item as CFDictionary, attributes(data) as CFDictionary)
        if status == errSecItemNotFound {
            status = SecItemAdd(item.merging(attributes(data)) { name, _ in name } as CFDictionary, nil)
        }
        return status == errSecSuccess
    }

    /// The item's value, nil where there is none or it cannot be read.
    static func read() -> Data? {
        var query = item
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var value: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &value) == errSecSuccess else { return nil }
        return value as? Data
    }

    // MARK: The value

    /// A timeline as the item holds it: its JSON, compressed. A day of
    /// many meetings is over a hundred kilobytes as JSON and a few as this,
    /// which is what a Keychain item should be.
    static func pack(_ timeline: GlanceTimeline) throws -> Data {
        try (timeline.encoded() as NSData).compressed(using: .zlib) as Data
    }

    /// The timeline an item's value holds, nil for one this build cannot read.
    static func unpack(_ data: Data) -> GlanceTimeline? {
        guard let json = try? (data as NSData).decompressed(using: .zlib) as Data else { return nil }
        return try? GlanceTimeline(data: json)
    }

    /// The timeline the item holds now.
    static func timeline() -> GlanceTimeline? { read().flatMap(unpack) }
}
