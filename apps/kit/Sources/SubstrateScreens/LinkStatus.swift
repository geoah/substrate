import Foundation
import SubstrateAuth
import SubstrateLocal

/// Where the app stands with its repository, most blocking first, in one
/// sentence (Workouts' `SyncStatus`, for any app).
public enum LinkStatus: Codable, Hashable, Sendable {
    /// No server: the app keeps everything on the device.
    case local
    /// Signed in; the app's package is being checked.
    case preparing
    /// The token was refused or its access ended: sign in again.
    case signedOut
    /// The token lacks a scope the app needs, or a package it needs waits
    /// for the owner's approval.
    case needsApproval
    /// The server cannot take what the app needs: update it.
    case serverTooOld
    /// The device could not keep a change, or its queue, on disk.
    case notKept(String)
    /// The repository holds an older version of the app's package than this
    /// build was written against, and the server has not upgraded it yet.
    case packageBehind(held: Int, known: Int)
    /// Ready, with this many writes still waiting.
    case ready(waiting: Int)
    /// Ready, and the approved access ends on this date.
    case accessEndsSoon(Date, waiting: Int)
    /// The server could not be reached; writes wait.
    case offline(waiting: Int, reason: String)
    /// The server refused a write, which waits, parked, with its words.
    case refused(waiting: Int, reason: String)

    public static func derive(signedIn: Bool, package: HeldPackage?, outbox: OutboxState, waiting: Int, known: Int,
                              issues: [AppIssue] = [], notKept: String? = nil) -> LinkStatus
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
        guard let package else { return .preparing }
        if case let .behind(held) = package { return .packageBehind(held: held, known: known) }
        guard package.writable else { return .preparing }
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

/// What the Status screen's sentences name: the app, the device, the
/// package and what the device keeps while it waits.
public struct StatusWords: Codable, Hashable, Sendable {
    public var app: String
    /// "this iPhone", "this Mac", "this device".
    public var device: String
    /// The package by its name: "Training".
    public var package: String
    /// What the app keeps on the device while it is signed out or waiting:
    /// "sessions", "tasks".
    public var kept: String

    public init(app: String, device: String, package: String, kept: String) {
        self.app = app
        self.device = device
        self.package = package
        self.kept = kept
    }

    /// One sentence for Settings.
    public func sentence(_ status: LinkStatus, locale: Locale, timeZone: TimeZone) -> String {
        func changes(_ count: Int) -> String { count == 1 ? "change" : "changes" }
        switch status {
        case .local:
            return "Not signed in. \(kept.capitalizedFirst) stay on \(device)."
        case .preparing:
            return "Checking the \(package) package on your substrate."
        case .signedOut:
            return "Your access ended. Sign in again to send what waits."
        case .needsApproval:
            return "\(app) needs your approval for more access. Review it to send what waits."
        case .serverTooOld:
            return "Your substrate needs an update before \(app) can use it. \(kept.capitalizedFirst) wait on \(device) until then."
        case let .notKept(reason):
            return "A change could not be kept on \(device): \(reason)"
        case let .accessEndsSoon(date, waiting):
            return "Access ends \(DateWords.date(date, locale: locale, timeZone: timeZone)). Renew it to keep sending."
                + (waiting == 0 ? "" : " Sending \(waiting) \(changes(waiting)).")
        case let .packageBehind(held, known):
            return "Your substrate has version \(held) of the \(package) package and this app needs version \(known) or later. "
                + "The update waits for your approval on your substrate; \(kept) wait on \(device) until then."
        case let .ready(waiting):
            return waiting == 0 ? "Up to date." : "Sending \(waiting) \(changes(waiting))."
        case let .offline(waiting, _):
            return "Your substrate cannot be reached. \(waiting) \(waiting == 1 ? "change waits" : "changes wait") on \(device)."
        case let .refused(waiting, reason):
            return "Your substrate refused a change: \(reason) \(waiting) \(waiting == 1 ? "change waits" : "changes wait"); "
                + "retry or discard it below."
        }
    }
}
