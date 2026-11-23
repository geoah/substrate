#if os(macOS)
import Foundation
import Security
import Testing
import TodayCore
import TodayGlance

@testable import SubstrateTodayUI

/// The Keychain item the widgets' day is kept in, as far as it is held with
/// no Keychain: what names the item, what a write sets, the group the
/// entitlements give both sides, and the value there and back.
@Suite struct GlanceKeychainTests {
    @Test func theItemIsOneGenericPasswordOnThisDeviceAndNamesNoGroup() {
        let item = GlanceKeychain.item
        #expect(item[kSecClass as String] as? String == kSecClassGenericPassword as String)
        #expect(item[kSecAttrService as String] as? String == "dev.reamde.substrate.today.glance")
        #expect(item[kSecAttrAccount as String] as? String == "day")
        // Never synchronised to iCloud.
        #expect(item[kSecAttrSynchronizable as String] as? Bool == false)
        // The group is the entitlements' to name: a write goes to the first one listed, a read searches it.
        #expect(item[kSecAttrAccessGroup as String] == nil && item.count == 4)
        let value = Data([1, 2, 3])
        let set = GlanceKeychain.attributes(value)
        #expect(set[kSecAttrAccessible as String] as? String == kSecAttrAccessibleAfterFirstUnlock as String)
        #expect(set[kSecValueData as String] as? Data == value && set.count == 2)
    }

    /// The code names no group, so the entitlements carry it: the app,
    /// in a local build and in a release, and the extension each list the
    /// app's own identifier as their one Keychain group, and none asks for
    /// an App Group, which the release's signing cannot register.
    @Test func theAppAndTheExtensionListTheAppsOwnKeychainGroupAndNoAppGroup() throws {
        let support = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("../Support").standardized
        for name in ["SubstrateToday-iOS", "SubstrateToday-iOS-site", "SubstrateTodayWidgets"] {
            let data = try Data(contentsOf: support.appendingPathComponent("\(name).entitlements"))
            let entitlements = try #require(try PropertyListSerialization.propertyList(from: data, format: nil) as? [String: Any], "\(name)")
            #expect(entitlements["keychain-access-groups"] as? [String] == ["$(AppIdentifierPrefix)dev.reamde.substrate.today"], "\(name)")
            #expect(entitlements["com.apple.security.application-groups"] == nil, "\(name)")
        }
    }

    @Test func aDayPacksToAFewKilobytesAndReadsBackAsWritten() throws {
        // The busiest moment of the invented day: thirteen tasks and seventeen meetings ahead.
        let timeline = GlancePresenter.timeline(TodayFixtures.state(at: "09:05"), now: TodayFixtures.at("09:05"), calendar: TodayFixtures.calendar)
        let json = try timeline.encoded()
        let packed = try GlanceKeychain.pack(timeline)
        print("GLANCE \(timeline.entries.count) entries are \(json.count) bytes as JSON and \(packed.count) bytes in the item")
        #expect(GlanceKeychain.unpack(packed) == timeline)
        #expect(packed.count * 8 < json.count && packed.count < 24_000)
        let closed = GlanceTimeline.closed(at: TodayFixtures.at("11:30"))
        #expect(GlanceKeychain.unpack(try GlanceKeychain.pack(closed)) == closed)
        // What is no packed timeline reads as none, and the widgets then say to open the app.
        #expect(GlanceKeychain.unpack(json) == nil && GlanceKeychain.unpack(Data()) == nil && GlanceKeychain.unpack(Data([0, 1, 2, 3])) == nil)
    }
}
#endif
