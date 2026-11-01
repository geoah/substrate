import AuthenticationServices
import Foundation
#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// The system's sign-in sheet, sharing the browser's cookies, so a person
/// already signed in to their substrate in Safari only approves. It never
/// sees the loopback redirect as its own callback: the receiver does, and
/// the sheet is closed when it has.
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

    public init(callbackScheme: String? = nil) {
        self.callbackScheme = callbackScheme ?? Bundle.main.bundleIdentifier?.lowercased() ?? "dev.reamde.substrate"
    }

    public func run(_ url: URL, receiver: LoopbackReceiver) async throws -> [String: String] {
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
