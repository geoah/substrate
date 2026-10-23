import Combine
import SwiftUI

#if os(macOS)
import Sparkle
#else
import UIKit
#endif

/// What this build is, as `apple.sh release` stamped it. A build nobody
/// released has no update address and never looks for one.
struct BuildInfo: Sendable {
    /// The full version, pre-release and commit included where it has them.
    let version: String
    /// CFBundleVersion: what one release is compared to another by.
    let build: String
    let channel: String
    let commit: String
    /// The release folder: `index.json` and `appcast.xml` sit at its root, and
    /// every path in them is relative to it.
    let updates: URL?

    static let current = BuildInfo(Bundle.main.infoDictionary ?? [:])

    init(_ info: [String: Any]) {
        func value(_ key: String) -> String {
            (info[key] as? String)?.trimmingCharacters(in: .whitespaces) ?? ""
        }
        let short = value("CFBundleShortVersionString")
        build = value("CFBundleVersion")
        let stamped = value("SubstrateVersion")
        version = stamped.isEmpty ? "\(short) (\(build))" : stamped
        channel = value("SubstrateChannel")
        commit = value("SubstrateCommit")
        let base = value("SubstrateUpdates")
        updates = base.isEmpty ? nil : URL(string: base.hasSuffix("/") ? base : base + "/")
    }

    static func newer(_ a: String, than b: String) -> Bool {
        a.compare(b, options: .numeric) == .orderedDescending
    }
}

/// `index.json`: every published release, newest first.
struct ReleaseIndex: Decodable {
    struct Release: Decodable, Identifiable {
        let version: String
        let build: String
        let channel: String
        let commit: String
        let ios: Artifact?
        var id: String { build }
    }

    struct Artifact: Decodable {
        let path: String
        /// iOS: the over-the-air install manifest.
        let manifest: String?
    }

    let releases: [Release]
}

/// Keeps a released build current. On macOS Sparkle checks, downloads,
/// verifies and installs; on iOS the app reads `index.json` and offers the
/// newest release's install link, because only the system may install an app.
@MainActor
final class Updater: NSObject, ObservableObject {
    nonisolated static let prereleasesKey = "updates.prereleases"
    private static let skippedKey = "updates.skipped"

    let build = BuildInfo.current
    var enabled: Bool { build.updates != nil }

    /// Pre-releases are offered too. A pre-release build starts with it on.
    @Published var prereleases: Bool {
        didSet { UserDefaults.standard.set(prereleases, forKey: Self.prereleasesKey) }
    }
    @Published private(set) var canCheck = false
    /// The outcome of a check somebody asked for; a scheduled one stays quiet.
    @Published var note: String?
    /// iOS: a newer release this device can install.
    @Published var offer: ReleaseIndex.Release?

    override init() {
        UserDefaults.standard.register(defaults: [Self.prereleasesKey: BuildInfo.current.channel != "stable"])
        prereleases = UserDefaults.standard.bool(forKey: Self.prereleasesKey)
        super.init()
        #if os(macOS)
        let key = Bundle.main.object(forInfoDictionaryKey: "SUPublicEDKey") as? String ?? ""
        if enabled, !key.isEmpty {
            let controller = SPUStandardUpdaterController(
                startingUpdater: true, updaterDelegate: self, userDriverDelegate: nil)
            controller.updater.publisher(for: \.canCheckForUpdates).assign(to: &$canCheck)
            self.controller = controller
            // Sparkle's schedule only checks once the interval has passed
            // since the last check; a launch checks regardless.
            if !controller.updater.sessionInProgress {
                controller.updater.checkForUpdatesInBackground()
            }
        }
        #else
        canCheck = enabled
        #endif
    }

    #if os(macOS)
    private var controller: SPUStandardUpdaterController?

    func checkNow() {
        controller?.checkForUpdates(nil)
    }
    #else
    private var lastCheck: Date?

    func checkNow() {
        Task { await check(asked: true) }
    }

    /// At launch and on every return to the foreground, at most hourly.
    func checkIfDue() {
        guard enabled else { return }
        if let lastCheck, Date().timeIntervalSince(lastCheck) < 3600 { return }
        Task { await check(asked: false) }
    }

    func skip(_ release: ReleaseIndex.Release) {
        UserDefaults.standard.set(release.build, forKey: Self.skippedKey)
        offer = nil
    }

    func install(_ release: ReleaseIndex.Release) {
        offer = nil
        guard let base = build.updates, let manifest = release.ios?.manifest,
              let url = URL(string: manifest, relativeTo: base)?.absoluteString
        else { return }
        var allowed = CharacterSet.urlQueryAllowed
        allowed.remove(charactersIn: "&=?+")
        let encoded = url.addingPercentEncoding(withAllowedCharacters: allowed) ?? url
        guard let link = URL(string: "itms-services://?action=download-manifest&url=\(encoded)") else { return }
        UIApplication.shared.open(link)
    }

    private func check(asked: Bool) async {
        guard let base = build.updates, canCheck else { return }
        canCheck = false
        defer { canCheck = true }
        lastCheck = Date()
        note = nil
        do {
            let request = URLRequest(
                url: base.appendingPathComponent("index.json"),
                cachePolicy: .reloadIgnoringLocalAndRemoteCacheData, timeoutInterval: 20)
            let (data, response) = try await URLSession.shared.data(for: request)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw URLError(.badServerResponse) }
            let newest = try JSONDecoder().decode(ReleaseIndex.self, from: data).releases
                .filter { $0.ios != nil && ($0.channel == "stable" || prereleases) }
                .max { BuildInfo.newer($1.build, than: $0.build) }
            guard let newest, BuildInfo.newer(newest.build, than: build.build) else {
                if asked { note = "This is the newest release." }
                return
            }
            if asked || UserDefaults.standard.string(forKey: Self.skippedKey) != newest.build {
                offer = newest
            }
        } catch {
            if asked { note = "The check failed: \(error.localizedDescription)" }
        }
    }
    #endif
}

#if os(macOS)
extension Updater: SPUUpdaterDelegate {
    nonisolated func feedURLString(for updater: SPUUpdater) -> String? {
        BuildInfo.current.updates?.appendingPathComponent("appcast.xml").absoluteString
    }

    /// A release without a channel is stable and every build sees it; the
    /// release script puts pre-releases on `prerelease`.
    nonisolated func allowedChannels(for updater: SPUUpdater) -> Set<String> {
        UserDefaults.standard.bool(forKey: Updater.prereleasesKey) ? ["prerelease"] : []
    }
}

struct UpdateCommands: Commands {
    @ObservedObject var updates: Updater

    var body: some Commands {
        CommandGroup(after: .appInfo) {
            if updates.enabled {
                Button("Check for Updates…") { updates.checkNow() }
                    .disabled(!updates.canCheck)
                Toggle("Include Pre-releases", isOn: $updates.prereleases)
            }
        }
    }
}
#endif

/// The build's version and the manual check, under the server picker.
struct BuildFooter: View {
    @EnvironmentObject private var updates: Updater

    var body: some View {
        // A pre-release's version is long: on its own line it wraps whole
        // rather than being squeezed beside the label.
        VStack(alignment: .leading, spacing: 4) {
            Text("Version").foregroundStyle(.secondary)
            Text(updates.build.version).monospaced().textSelection(.enabled)
            if updates.enabled {
                Button("Check for Updates") { updates.checkNow() }
                    .buttonStyle(.borderless)
                    .disabled(!updates.canCheck)
            }
        }
        .font(.footnote)
    }
}

extension View {
    /// iOS: the offer of a newer release, and the answer to a check somebody
    /// asked for, wherever the app is.
    func updateOffer(_ updates: Updater) -> some View {
        #if os(iOS)
        alert(
            "Update available",
            isPresented: Binding(get: { updates.offer != nil }, set: { if !$0 { updates.offer = nil } }),
            presenting: updates.offer
        ) { release in
            Button("Install") { updates.install(release) }
            Button("Not Now", role: .cancel) { updates.skip(release) }
        } message: { release in
            Text("Substrate \(release.version) is ready to install. You have \(updates.build.version).")
        }
        .alert(
            "Check for Updates",
            isPresented: Binding(get: { updates.note != nil }, set: { if !$0 { updates.note = nil } }),
            presenting: updates.note
        ) { _ in
            Button("OK", role: .cancel) {}
        } message: { note in
            Text(note)
        }
        #else
        self
        #endif
    }
}
