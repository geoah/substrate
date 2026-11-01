import AuthenticationServices
import Foundation
#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// Where the owner signs in and approves. On macOS, their default browser:
/// the authentication sheet there is a bare web view fastened to the app's
/// window, with no address bar to tell it from the app and no password
/// manager extension. On iOS, the system's Safari sheet, which names the
/// site and fills passwords, since an app the person switched away from is
/// suspended and its loopback port with it. Either way the receiver takes
/// the redirect, never the sheet or the browser.
@MainActor
public final class WebAuthenticator {
    public struct Cancelled: LocalizedError, Sendable {
        public var errorDescription: String? { "Sign-in was cancelled." }
    }

    /// A scheme the sheet waits on and nothing ever opens: the redirect is a
    /// loopback URL the receiver takes.
    private let callbackScheme: String
    /// The session holds its provider weakly; this keeps it for the run.
    private var anchor: Anchor?

    /// Whether the sign-in leaves the app for the default browser, so a
    /// screen waiting on it offers its own Cancel: there is no sheet to close.
    public static var opensBrowser: Bool {
        #if os(macOS)
        true
        #else
        false
        #endif
    }

    public init(callbackScheme: String? = nil) {
        self.callbackScheme = callbackScheme ?? Bundle.main.bundleIdentifier?.lowercased() ?? "dev.reamde.substrate"
    }

    public func run(_ url: URL, receiver: LoopbackReceiver) async throws -> [String: String] {
        #if os(macOS)
        guard NSWorkspace.shared.open(url) else { throw TransportError(text: "Your browser couldn't open the sign-in page.") }
        // Cancelling the task is the only way out: a closed tab never comes back.
        let query = try await receiver.next()
        NSApplication.shared.activate()
        return query
        #else
        guard let window = Self.window() else { throw TransportError(text: "The sign-in sheet couldn't open: no window is showing.") }
        let anchor = Anchor(window)
        self.anchor = anchor
        defer { self.anchor = nil }
        let gate = ResumeGate<[String: String]>()
        return try await withCheckedThrowingContinuation { continuation in
            gate.set(continuation)
            let session = ASWebAuthenticationSession(url: url, callback: .customScheme(callbackScheme)) { _, error in
                // The person closed the sheet, or the receiver's close below.
                gate.resume(.failure(error.map { ($0 as? ASWebAuthenticationSessionError)?.code == .canceledLogin ? Cancelled() : $0 }
                        ?? Cancelled()))
            }
            session.presentationContextProvider = anchor
            session.prefersEphemeralWebBrowserSession = false
            let waiter = Task {
                do {
                    let query = try await receiver.next()
                    gate.resume(.success(query))
                } catch {
                    gate.resume(.failure(error))
                }
                session.cancel()
            }
            if !session.start() {
                waiter.cancel()
                gate.resume(.failure(TransportError(text: "The sign-in sheet couldn't open.")))
            }
        }
        #endif
    }

    /// The window the sheet shows over: the key one, else any.
    static func window() -> ASPresentationAnchor? {
        #if os(macOS)
        NSApplication.shared.keyWindow ?? NSApplication.shared.windows.first
        #else
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.flatMap(\.windows).first(where: \.isKeyWindow) ?? scenes.first.map { UIWindow(windowScene: $0) }
        #endif
    }

    @MainActor
    private final class Anchor: NSObject, ASWebAuthenticationPresentationContextProviding {
        let window: ASPresentationAnchor

        init(_ window: ASPresentationAnchor) {
            self.window = window
        }

        func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor { window }
    }
}

/// A continuation resumed by whichever of its racers gets there first.
final class ResumeGate<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<T, Error>?

    func set(_ c: CheckedContinuation<T, Error>) {
        lock.lock()
        continuation = c
        lock.unlock()
    }

    func resume(_ result: Result<T, Error>) {
        lock.lock()
        let c = continuation
        continuation = nil
        lock.unlock()
        c?.resume(with: result)
    }
}
