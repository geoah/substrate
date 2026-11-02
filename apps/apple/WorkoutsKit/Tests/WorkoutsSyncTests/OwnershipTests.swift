import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// A training package that is not the app's (decision 0205): the owner took
/// it over, or another app declared it. The app stops applying, never in a
/// loop, keeps its records writable, and Settings says who keeps it.
@Suite("A package managed elsewhere")
struct OwnershipTests {
    static let clientID = "https://workouts.example.org/client.json"
    static let packagePath = "/api/v1/substrate.reamde.dev/core/package/ada.example.com%2Ftraining"

    /// A package row at `version` carrying these ownership properties.
    static func row(_ version: Int, _ ownership: String) -> String {
        #"{"id":"ada.example.com/training","kind":"substrate.reamde.dev/core/package","properties":{"version":\#(version)\#(ownership.isEmpty ? "" : "," + ownership)},"version":3}"#
    }

    static func installer(_ server: ScriptedServer) throws -> PackageInstaller {
        PackageInstaller(api: api(server), manifest: try PackageManifest.bundled(), clientID: clientID)
    }

    @Test("a package the owner took over is not applied, and its records still go")
    func takenOver() async throws {
        let shipped = try PackageManifest.bundled().version
        let server = ScriptedServer()
        server.answer("GET", Self.packagePath, 200, Self.row(shipped - 1, #""declaredBy":"console","takenOverFrom":"\#(Self.clientID)""#))
        let state = try await Self.installer(server).ensure()
        #expect(state == .managedElsewhere(held: shipped - 1, by: .owner(takenOverFrom: Self.clientID)))
        #expect(!server.sent.contains { $0.method == "POST" })
        #expect(state.writable)
        let status = SyncStatus.derive(signedIn: true, package: state, outbox: .sent, waiting: 2, known: shipped)
        #expect(status == .packageManagedElsewhere(.owner(takenOverFrom: Self.clientID), waiting: 2))
        #expect(status.sentence.contains("managed by you now (taken over); this app no longer updates it"))
        #expect(status.sentence.contains("Sending 2 changes"))
        // A write the server refuses, or the network, still says so first.
        #expect(SyncStatus.derive(signedIn: true, package: state, outbox: .offline("down"), waiting: 1, known: shipped)
            == .offline(waiting: 1, reason: "down"))
    }

    @Test("a package another app declared is not applied, from the same host too")
    func anotherApp() async throws {
        let shipped = try PackageManifest.bundled().version
        for (ownership, app) in [
            (#""declaredBy":"app:notes.example.org","declaredByClient":"https://notes.example.org/client.json""#, "https://notes.example.org/client.json"),
            (#""declaredBy":"app:workouts.example.org","declaredByClient":"https://workouts.example.org/planner/client.json""#, "https://workouts.example.org/planner/client.json"),
            (#""declaredBy":"app:notes.example.org""#, "app:notes.example.org"),
        ] {
            let server = ScriptedServer()
            server.answer("GET", Self.packagePath, 200, Self.row(shipped - 1, ownership))
            let state = try await Self.installer(server).ensure()
            #expect(state == .managedElsewhere(held: shipped - 1, by: .otherApp(app)))
            #expect(!server.sent.contains { $0.method == "POST" })
            #expect(PackageManager.otherApp(app).sentence.contains("managed by another app, \(app)"))
        }
    }

    @Test("the app's own package, or one its host declared before the client id was stamped, is upgraded")
    func ours() async throws {
        let shipped = try PackageManifest.bundled().version
        for ownership in [
            #""declaredBy":"app:workouts.example.org","declaredByClient":"\#(Self.clientID)""#,
            #""declaredBy":"app:workouts.example.org""#,
        ] {
            let server = ScriptedServer()
            let applied = Locked(false)
            server.on { sent in
                guard sent.method == "GET", sent.path == Self.packagePath else { return nil }
                return (200, Self.row(applied.value ? shipped : shipped - 1, ownership))
            }
            server.on { sent in
                guard sent.method == "POST", sent.path == "/api/v1/vocabulary/apply" else { return nil }
                applied.value = true
                return (200, #"{"records":[]}"#)
            }
            server.on { sent in sent.method == "GET" ? (404, #"{"error":{"code":"not_found","message":"not found"}}"#) : nil }
            #expect(try await Self.installer(server).ensure() == .current(held: shipped))
            #expect(server.sent.filter { $0.method == "POST" }.count == 1)
        }
    }

    @Test("a 403 on apply stops the applying: the next check reads who keeps it and applies nothing")
    func forbiddenApply() async throws {
        let shipped = try PackageManifest.bundled().version
        let server = ScriptedServer()
        let takenOver = Locked(false)
        server.on { sent in
            guard sent.method == "GET", sent.path == Self.packagePath else { return nil }
            return (200, Self.row(shipped - 1, takenOver.value
                    ? #""declaredBy":"console","takenOverFrom":"\#(Self.clientID)""#
                    : #""declaredBy":"app:workouts.example.org","declaredByClient":"\#(Self.clientID)""#))
        }
        server.on { sent in
            guard sent.method == "POST", sent.path == "/api/v1/vocabulary/apply" else { return nil }
            // The owner took it over between the app's read and its apply.
            takenOver.value = true
            return (403, #"{"error":{"code":"forbidden","message":"ada.example.com/training is the owner's package"}}"#)
        }
        server.on { sent in sent.method == "GET" ? (404, #"{"error":{"code":"not_found","message":"not found"}}"#) : nil }
        let installer = try Self.installer(server)
        let first = try await installer.ensure()
        #expect(first == .managedElsewhere(held: shipped - 1, by: .owner(takenOverFrom: Self.clientID)))
        #expect(try await installer.ensure() == first)
        #expect(try await installer.ensure() == first)
        #expect(server.sent.filter { $0.method == "POST" }.count == 1)
    }

    @Test("a 403 the row does not explain is said in the server's words, and still writable")
    func unexplained() async throws {
        let shipped = try PackageManifest.bundled().version
        let server = ScriptedServer()
        server.answer("GET", Self.packagePath, 200, Self.row(shipped - 1, ""))
        server.answer("POST", "/api/v1/vocabulary/apply", 403,
                      #"{"error":{"code":"forbidden","message":"ada.example.com/training is managed by another app, https://notes.example.org/client.json, which declared it"}}"#)
        server.on { sent in sent.method == "GET" ? (404, #"{"error":{"code":"not_found","message":"not found"}}"#) : nil }
        let state = try await Self.installer(server).ensure()
        guard case let .managedElsewhere(held, .unknown(reason)) = state else { Issue.record("state \(state)"); return }
        #expect(held == shipped - 1 && reason.contains("managed by another app"))
        #expect(state.writable)
    }
}
