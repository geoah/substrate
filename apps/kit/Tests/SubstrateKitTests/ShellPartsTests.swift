#if canImport(SwiftUI)
import Foundation
import Testing
@testable import SubstrateAuth
@testable import SubstrateLocal
@testable import SubstrateScreens
@testable import SubstrateUI
import SubstrateTesting

/// The shell is made over whatever an app laid out before the kit: a path on
/// the device never moves (docs/plans/app-kit.md §6), so an app hands the
/// shell its folder and outboxes, and the link sends from them.
@MainActor
@Suite("The shell's parts", .timeLimit(.minutes(10)))
struct ShellPartsTests {
    typealias Shell = AppShell<NoApp>

    let session = Session(server: URL(string: "https://ada.example.com")!, repository: "ada.example.com", secret: "substrate_tok_test")
    let sessions = SessionStore(storage: MemoryStorage())

    func configuration(_ parts: @escaping (Session?) -> Shell.Parts) -> Shell.Configuration {
        Shell.Configuration(manifest: AppManifest(appName: "Notes", clientID: "http://127.0.0.1/notes", scopes: ["write:example.com/notes/*"]),
                            keychainService: "test.notes", parts: parts, packages: [:],
                            words: { StatusWords(app: "Notes", device: $0, kept: "notes") }, app: { _ in NoApp() },
                            settingsScreen: { link, _ in SettingsPresenter.screen([], link: link, storage: nil, about: Self.about) })
    }

    static let about = AboutPresentation.present(appName: "Notes", version: "1.0", channel: nil, offered: nil)

    @Test func aWorkoutsStyleOutboxPerAccountIsTheOneTheLinkSendsFrom() throws {
        let root = temporaryFolder("shell")
        let account = root.appendingPathComponent("accounts/ada", isDirectory: true)
        try FileManager.default.createDirectory(at: account, withIntermediateDirectories: true)
        let outbox = Outbox(folder: try RepositoryFolder(adopting: account), name: "outbox.json", area: .root)
        let configuration = configuration { session in
            guard session != nil else { return Shell.Parts() }
            return Shell.Parts(outboxes: { $0.repository == "ada.example.com" ? outbox : nil })
        }
        let parts = Shell.parts(configuration, session: session, device: "this iPhone", build: BuildFacts(version: "1.0"))
        #expect(parts.words.device == "this iPhone")
        let link = Shell.link(configuration, parts: parts, sessions: sessions)
        #expect(link.outbox == nil)
        #expect(link.outboxes?(SessionFacts(session)) === outbox)
        #expect(link.words == parts.words)
    }

    @Test func aChatStyleFolderIsAdoptedWhereTheAppKeptIt() throws {
        let kept = temporaryFolder("shell").appendingPathComponent("SubstrateChat/ada", isDirectory: true)
        try FileManager.default.createDirectory(at: kept, withIntermediateDirectories: true)
        try Data(#"{"drafts":[]}"#.utf8).write(to: kept.appendingPathComponent("drafts.json"))
        let configuration = configuration { session in
            guard session != nil, let folder = try? RepositoryFolder(adopting: kept) else { return Shell.Parts() }
            return Shell.Parts(folder: folder, cache: RecordCache(folder: folder, name: "watch.json"), outbox: Outbox(folder: folder))
        }
        let parts = Shell.parts(configuration, session: session, device: "this Mac", build: BuildFacts(version: "1.0"))
        #expect(parts.folder?.root.standardizedFileURL == kept.standardizedFileURL)
        // Adopted as it was: what the app kept there stays, and no marker is written beside it.
        #expect(FileManager.default.fileExists(atPath: kept.appendingPathComponent("drafts.json").path))
        #expect(try FileManager.default.contentsOfDirectory(atPath: kept.path) == ["drafts.json"])
        let link = Shell.link(configuration, parts: parts, sessions: sessions)
        #expect(link.outbox === parts.outbox)
        #expect(link.outboxes == nil)
    }

    @Test func theKitsLayoutIsOneFolderPerRepositoryAndNoneSignedOut() {
        let layout = Shell.Parts.repositoryFolders(in: "SubstrateShellPartsTests")
        let none = layout(nil)
        #expect(none.folder == nil && none.outbox == nil && none.cache == nil)
        let made = layout(session)
        let name = RepositoryFolder.name(server: session.server, repository: session.repository)
        #expect(made.folder?.root.lastPathComponent == name)
        #expect(made.folder?.root.deletingLastPathComponent().lastPathComponent == "SubstrateShellPartsTests")
        if let root = made.folder?.root.deletingLastPathComponent() { try? FileManager.default.removeItem(at: root) }
    }
}
#endif
