import Foundation
import Observation

/// Whether an app is still set up with its server, from what the server
/// says now: its discovery and metadata against the app's requirement,
/// `POST /api/v1/authorizations/check` against the app's request, and when
/// the access ends. A row the token cannot see (`unknown`) is never a reason
/// to ask the owner anything.
public struct AppStatusCheck: Sendable {
    public var manifest: AppManifest
    public var session: Session
    public var client: SubstrateClient
    public var now: @Sendable () -> Date
    /// How far ahead an ending is worth telling the owner about.
    public var endingWindow: TimeInterval

    public init(manifest: AppManifest, session: Session, client: SubstrateClient? = nil,
                now: @escaping @Sendable () -> Date = { Date() }, endingWindow: TimeInterval = 7 * 86400)
    {
        self.manifest = manifest
        self.session = session
        self.client = client ?? SubstrateClient(session)
        self.now = now
        self.endingWindow = endingWindow
    }

    public func run() async -> AppStatus {
        let at = now()
        if session.hasEnded(at: at) { return AppStatus(issues: [.accessEnded], checkedAt: at) }
        let probe: ServerProbe.Result
        do {
            probe = try await ServerProbe(transport: client.transport)
                .probe(session.server, requirement: manifest.requirement, app: manifest.appName)
        } catch {
            return AppStatus(issues: [.unreachable(message: words(error))] + ending(session.expiresAt, at: at), checkedAt: at)
        }
        var issues: [AppIssue] = []
        var expiresAt = session.expiresAt
        switch probe.verdict {
        case let .tooOld(running, needed, missing):
            // A password session is the right one on a server the app falls
            // back for: nothing to tell the owner until it is updated.
            if session.isApp || manifest.door(for: probe) != .password {
                issues.append(.serverTooOld(running: running, needed: needed, missing: missing))
            }
        case .compatible:
            if !session.isApp {
                issues.append(.legacyAccess)
            } else {
                do {
                    let result = try await client.checkAuthorization(manifest.check(for: probe.metadata))
                    let found = manifest.changes(from: result, session: session, metadata: probe.metadata)
                    if !found.serverMissing.isEmpty {
                        issues.append(.serverTooOld(running: probe.version, needed: nil, missing: found.serverMissing))
                    }
                    if !found.changes.isEmpty { issues.append(.needsApproval(changes: found.changes)) }
                    expiresAt = result.expiresAt ?? expiresAt
                } catch is SessionExpired {
                    return AppStatus(issues: [.accessEnded], serverVersion: probe.version, checkedAt: at)
                } catch let error as ServerError where error.status == 404 || error.status == 405 {
                    // A server between app authorization and the check: it
                    // has nothing to say about set up, and the scopes were
                    // approved as the app asked.
                } catch {
                    issues.append(.unreachable(message: words(error)))
                }
            }
        }
        issues += ending(expiresAt, at: at)
        return AppStatus(issues: issues.sorted { Self.rank($0) < Self.rank($1) }, serverVersion: probe.version, checkedAt: at)
    }

    private func ending(_ expiresAt: Date?, at: Date) -> [AppIssue] {
        guard let expiresAt else { return [] }
        if expiresAt <= at { return [.accessEnded] }
        return expiresAt.timeIntervalSince(at) <= endingWindow ? [.accessEndsSoon(expiresAt)] : []
    }

    private func words(_ error: Error) -> String {
        (error as? ServerProbe.NotASubstrate)?.reason ?? explain(error, app: manifest.appName, server: session.server)
    }

    /// What the owner meets first: an ended access outranks everything, a
    /// server update comes before an approval it would block.
    static func rank(_ issue: AppIssue) -> Int {
        switch issue {
        case .accessEnded: 0
        case .serverTooOld: 1
        case .needsApproval: 2
        case .accessEndsSoon: 3
        case .legacyAccess: 4
        case .unreachable: 5
        }
    }
}

/// An app's status kept current: checked at launch and on return to the
/// foreground, at most once per `interval` unless forced.
@MainActor
@Observable
public final class AppStatusModel {
    public private(set) var status: AppStatus?
    public private(set) var checking = false
    public var interval: TimeInterval
    private var make: () -> AppStatusCheck?
    private var lastRun: Date?

    /// `make` builds the check from the app's current session; nil while
    /// signed out.
    public init(interval: TimeInterval = 300, make: @escaping () -> AppStatusCheck?) {
        self.interval = interval
        self.make = make
    }

    public func refresh(force: Bool = false) async {
        guard !checking, let check = make() else { return }
        if !force, let lastRun, Date().timeIntervalSince(lastRun) < interval { return }
        checking = true
        defer { checking = false }
        lastRun = Date()
        status = await check.run()
    }

    /// Forgets the status: the session changed.
    public func reset() {
        status = nil
        lastRun = nil
    }
}
