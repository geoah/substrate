import Foundation
import SubstrateDevice

/// A browser sign-in that opens nothing: `answer` says what each page comes
/// back with, and every route made ready, page opened and route let go of is
/// kept for the test to read.
@MainActor
public final class ScriptedBrowser: BrowserSignIn {
    public private(set) var prepared: [SignInRedirect] = []
    public private(set) var opened: [URL] = []
    public private(set) var finished: [SignInRedirect] = []
    private let answer: (URL, SignInRedirect) -> SignInAnswer

    /// A loopback route answers on this port.
    public static let loopbackPort = 49152

    public init(answer: @escaping (URL, SignInRedirect) -> SignInAnswer = { _, _ in .cancelled }) {
        self.answer = answer
    }

    public func prepare(_ route: SignInRoute) async throws -> SignInRedirect {
        let redirect = switch route {
        case let .claimed(url): SignInRedirect(route: route, uri: url.absoluteString)
        case let .loopback(_, path): SignInRedirect(route: route, uri: "http://127.0.0.1:\(Self.loopbackPort)\(path)")
        }
        prepared.append(redirect)
        return redirect
    }

    public func open(_ page: URL, for redirect: SignInRedirect) async throws -> SignInAnswer {
        opened.append(page)
        return answer(page, redirect)
    }

    public func finish(_ redirect: SignInRedirect) {
        finished.append(redirect)
    }

    /// The query parameter `name` of an opened page, such as the `state` a
    /// callback must echo.
    public static func parameter(_ name: String, of page: URL) -> String? {
        URLComponents(url: page, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == name }?.value
    }
}
