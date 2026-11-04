import Foundation
import SubstrateAuth
import SubstrateCore
import SubstrateLocal

/// Where the app stands with its repository, most blocking first, in one
/// sentence.
public enum LinkStatus: Codable, Hashable, Sendable {
    /// No server: the app keeps everything on the device.
    case local
    /// Signed in; these packages, by name, are being checked.
    case preparing([String])
    /// The token was refused or its access ended: sign in again.
    case signedOut
    /// The token lacks a scope the app needs, or a package it needs waits
    /// for the owner's approval.
    case needsApproval
    /// The server cannot take what the app needs: update it.
    case serverTooOld
    /// The device could not keep a change, or its queue, on disk; the
    /// reason is the system's words, said apart from the sentence.
    case notKept(String)
    /// The repository holds older versions of these packages than this
    /// build was written against, and the server has not upgraded them yet.
    case packagesBehind([PackageStanding])
    /// Ready, with this many writes still waiting.
    case ready(waiting: Int)
    /// Ready, and the approved access ends on this date.
    case accessEndsSoon(Date, waiting: Int)
    /// The server could not be reached; writes wait.
    case offline(waiting: Int, reason: String)
    /// The server refused a write, which waits, parked, with its words.
    case refused(waiting: Int, reason: String)

    /// - Parameters:
    ///   - packages: each package the app needs, where the repository
    ///     stands with it; none for an app that writes no package's kinds.
    ///   - notKept: why a change, or the outbox, could not be kept on the
    ///     device: the outbox's save problem, or a journal's.
    public static func derive(signedIn: Bool, packages: [PackageStanding], outbox: OutboxState, waiting: Int, issues: [AppIssue] = [],
                              notKept: String? = nil) -> LinkStatus
    {
        guard signedIn else { return .local }
        if issues.contains(.accessEnded) { return .signedOut }
        switch outbox {
        case .signedOut: return .signedOut
        case .needsApproval: return .needsApproval
        default: break
        }
        if issues.contains(where: { if case .needsApproval = $0 { true } else { false } }) { return .needsApproval }
        if issues.contains(where: { if case .serverTooOld = $0 { true } else { false } }) { return .serverTooOld }
        if let notKept { return .notKept(notKept) }
        let unread = packages.filter { $0.held == nil }
        if !unread.isEmpty { return .preparing(unread.map(\.name)) }
        let behind = packages.filter { if case .behind? = $0.held { true } else { false } }
        if !behind.isEmpty { return .packagesBehind(behind) }
        let missing = packages.filter { $0.held == .missing }
        if !missing.isEmpty { return .preparing(missing.map(\.name)) }
        switch outbox {
        case let .offline(reason): return .offline(waiting: waiting, reason: reason)
        case let .refused(reason): return .refused(waiting: waiting, reason: reason)
        default: break
        }
        for case let .accessEndsSoon(date) in issues {
            return .accessEndsSoon(date, waiting: waiting)
        }
        return .ready(waiting: waiting)
    }
}

/// One package the app needs, by its id and its name, with what the
/// repository holds of it (nil until read) and the version this build needs.
public struct PackageStanding: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    /// The package by its name: "Training".
    public var name: String
    public var held: HeldPackage?
    public var needs: Int

    public init(id: String, name: String, held: HeldPackage?, needs: Int) {
        self.id = id
        self.name = name
        self.held = held
        self.needs = needs
    }
}

/// What the Status screen's sentences name: the app, the device, what the
/// device keeps while it waits, and the packages by name.
public struct StatusWords: Codable, Hashable, Sendable {
    public var app: String
    /// "this iPhone", "this Mac", "this device".
    public var device: String
    /// What the app keeps on the device while it is signed out or waiting:
    /// "sessions", "tasks".
    public var kept: String
    /// A package's name by its id where the word built from the id is not
    /// the right one.
    public var packageNames: [String: String]
    /// The signed-out sentence, where the app says more than what it keeps
    /// (Workouts: the routines it carries, and the sessions).
    public var local: String?
    /// The Status screen's footer, where the app writes other than as the
    /// owner makes each change (Workouts writes a session when it finishes);
    /// `{server}` stands for the server's host.
    public var footer: String?
    /// The refused writes' footer, where Discard gives up more than the
    /// change and those behind it (Workouts: the rest of its session's).
    public var refusedFooter: String?
    /// What signing out gives up on the device beyond the sign-in, where
    /// the app gives something up (Chat: its saved chats), said in Sign
    /// Out's question.
    public var signOut: String?

    public init(app: String, device: String, kept: String, packageNames: [String: String] = [:], local: String? = nil,
                footer: String? = nil, refusedFooter: String? = nil, signOut: String? = nil)
    {
        self.signOut = signOut
        self.app = app
        self.device = device
        self.kept = kept
        self.packageNames = packageNames
        self.local = local
        self.footer = footer
        self.refusedFooter = refusedFooter
    }

    /// The Status screen's footer for the server at `host`.
    public func footer(host: String?) -> String {
        let server = host ?? "your substrate"
        guard let footer else { return "Changes are written to \(server) as you make them, and wait on \(device) while it cannot be reached." }
        return footer.replacingOccurrences(of: "{server}", with: server)
    }

    /// "Training" for `substrate.reamde.dev/training`, unless named.
    public func packageName(_ id: String) -> String {
        packageNames[id] ?? packageDisplayName(id.split(separator: "/").last.map(String.init) ?? id)
    }

    /// One sentence for Settings. A reason the status carries (a refusal's
    /// words, why a change was not kept) is not in it: the Status screen
    /// says it as a fact of its own.
    public func sentence(_ status: LinkStatus, locale: Locale, timeZone: TimeZone) -> String {
        func changes(_ count: Int) -> String { count == 1 ? "change" : "changes" }
        func wait(_ count: Int) -> String { "\(count) \(count == 1 ? "change waits" : "changes wait")" }
        switch status {
        case .local:
            return local ?? "Not signed in. \(kept.capitalizedFirst) stay on \(device)."
        case let .preparing(names):
            return "Checking the \(joinWords(names)) package\(names.count == 1 ? "" : "s") on your substrate."
        case .signedOut:
            return "Your access ended. Sign in again to send what waits."
        case .needsApproval:
            return "\(app) needs your approval for more access. Review it to send what waits."
        case .serverTooOld:
            return "Your substrate needs an update before \(app) can use it. \(kept.capitalizedFirst) wait on \(device) until then."
        case .notKept:
            return "A change could not be kept on \(device). \(kept.capitalizedFirst) already kept stay."
        case let .accessEndsSoon(date, waiting):
            return "Access ends \(DateWords.date(date, locale: locale, timeZone: timeZone)). Renew it to keep sending."
                + (waiting == 0 ? "" : " Sending \(waiting) \(changes(waiting)).")
        case let .packagesBehind(behind):
            if behind.count == 1, let package = behind.first, let held = package.held?.held {
                return "Your substrate has version \(held) of the \(package.name) package and this app needs version \(package.needs) or later. "
                    + "The update waits for your approval on your substrate; \(kept) wait on \(device) until then."
            }
            return "Your substrate has older versions of the \(joinWords(behind.map(\.name))) packages than this app needs. "
                + "The updates wait for your approval on your substrate; \(kept) wait on \(device) until then."
        case let .ready(waiting):
            return waiting == 0 ? "Up to date." : "Sending \(waiting) \(changes(waiting))."
        case let .offline(waiting, _):
            return "Your substrate cannot be reached. \(wait(waiting)) on \(device)."
        case let .refused(waiting, _):
            return "Your substrate refused a change. \(wait(waiting)); retry or discard it below."
        }
    }
}
