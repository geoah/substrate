import AuthenticationServices
import Foundation
#if os(macOS)
import AppKit
#else
import UIKit
import SubstrateKit
#endif

/// Receives the authorization redirect on a loopback port (RFC 8252 §7.3).
/// A native app has no web host to claim an https redirect with, and the
/// server admits `http` on a loopback host at any port, so the app listens on
/// 127.0.0.1 while the sign-in sheet is up and the browser lands here. A
/// plain socket, not Network.framework's listener: on iOS that listener
/// accepts at the kernel and never hands the connection over, under the
/// local network policy.
final class LoopbackReceiver: @unchecked Sendable {
    private let socket: Int32
    private let source: DispatchSourceRead
    private let queue = DispatchQueue(label: "dev.reamde.substrate.chat.oauth")
    private let lock = NSLock()
    private var waiting: CheckedContinuation<[String: String], Error>?
    private var received: Result<[String: String], Error>?
    let port: UInt16

    static let path = "/oauth/callback"

    var redirect: String { "http://127.0.0.1:\(port)\(Self.path)" }

    struct Unavailable: LocalizedError {
        let what: String
        var errorDescription: String? { "Substrate Chat couldn't listen for the sign-in to come back (\(what))." }
    }

    private init(socket: Int32, port: UInt16) {
        self.socket = socket
        self.port = port
        source = DispatchSource.makeReadSource(fileDescriptor: socket, queue: queue)
    }

    /// Listens on a free loopback port.
    static func start() async throws -> LoopbackReceiver {
        let fd = Darwin.socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { throw Unavailable(what: "socket \(errno)") }
        var address = sockaddr_in()
        address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        address.sin_family = sa_family_t(AF_INET)
        address.sin_port = 0
        address.sin_addr.s_addr = inet_addr("127.0.0.1")
        let bound = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) }
        }
        guard bound == 0, listen(fd, 8) == 0 else {
            close(fd)
            throw Unavailable(what: "bind \(errno)")
        }
        var length = socklen_t(MemoryLayout<sockaddr_in>.size)
        _ = withUnsafeMutablePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(fd, $0, &length) }
        }
        _ = fcntl(fd, F_SETFL, O_NONBLOCK)
        let receiver = LoopbackReceiver(socket: fd, port: UInt16(bigEndian: address.sin_port))
        receiver.source.setEventHandler { [weak receiver] in receiver?.accept() }
        receiver.source.setCancelHandler { close(fd) }
        receiver.source.resume()
        return receiver
    }

    /// The redirect's query parameters, once the browser arrives.
    func next() async throws -> [String: String] {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                lock.lock()
                if let received {
                    lock.unlock()
                    continuation.resume(with: received)
                    return
                }
                waiting = continuation
                lock.unlock()
            }
        } onCancel: {
            finish(.failure(CancellationError()))
        }
    }

    func stop() {
        source.cancel()
        finish(.failure(CancellationError()))
    }

    private func finish(_ result: Result<[String: String], Error>) {
        lock.lock()
        guard received == nil else { return lock.unlock() }
        received = result
        let continuation = waiting
        waiting = nil
        lock.unlock()
        continuation?.resume(with: result)
    }

    /// One connection: its request line read, the page written, then closed.
    /// The query is handed over only after the page is written, so the
    /// browser never meets a socket the app already closed.
    private func accept() {
        let client = Darwin.accept(socket, nil, nil)
        guard client >= 0 else { return }
        // An accepted socket inherits the listener's O_NONBLOCK; this one
        // reads the request whole, bounded by the timeout below.
        _ = fcntl(client, F_SETFL, fcntl(client, F_GETFL) & ~O_NONBLOCK)
        var timeout = timeval(tv_sec: 3, tv_usec: 0)
        setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
        var noSigPipe: Int32 = 1
        setsockopt(client, SOL_SOCKET, SO_NOSIGPIPE, &noSigPipe, socklen_t(MemoryLayout<Int32>.size))
        var request = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while request.count < 16 * 1024 {
            let n = read(client, &buffer, buffer.count)
            if n <= 0 { break }
            request.append(buffer, count: n)
            if request.range(of: Data("\r\n\r\n".utf8)) != nil { break }
        }
        let line = String(decoding: request, as: UTF8.self).split(separator: "\r\n").first.map(String.init) ?? ""
        let parts = line.split(separator: " ")
        let target = parts.count >= 2 ? String(parts[1]) : ""
        guard let components = URLComponents(string: "http://127.0.0.1\(target)"), components.path == Self.path else {
            write(client, status: "404 Not Found", body: "Not found.")
            close(client)
            return
        }
        var query: [String: String] = [:]
        for item in components.queryItems ?? [] { query[item.name] = item.value ?? "" }
        write(client, status: "200 OK", body: query["error"] != nil
            ? "Access was not granted. You can go back to Substrate Chat."
            : "Signed in. You can go back to Substrate Chat.")
        close(client)
        finish(.success(query))
    }

    private func write(_ client: Int32, status: String, body: String) {
        let page = """
        <!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">\
        <title>Substrate Chat</title></head><body style="font:17px -apple-system,system-ui;\
        display:flex;align-items:center;justify-content:center;height:90vh;color:#444">\(body)</body></html>
        """
        let bytes = Data(page.utf8)
        let head = "HTTP/1.1 \(status)\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: \(bytes.count)\r\n"
            + "Cache-Control: no-store\r\nConnection: close\r\n\r\n"
        let all = Data(head.utf8) + bytes
        all.withUnsafeBytes { raw in
            var sent = 0
            while sent < all.count {
                let n = Darwin.write(client, raw.baseAddress!.advanced(by: sent), all.count - sent)
                if n <= 0 { break }
                sent += n
            }
        }
    }
}

/// The system's sign-in sheet, sharing the browser's cookies, so a person
/// already signed in to their substrate in Safari only approves. It never
/// sees the loopback redirect as its own callback: the receiver does, and
/// the sheet is closed when it has.
@MainActor
final class WebSignIn: NSObject, ASWebAuthenticationPresentationContextProviding {
    struct Cancelled: LocalizedError {
        var errorDescription: String? { "Sign-in was cancelled." }
    }

    func run(_ url: URL, receiver: LoopbackReceiver) async throws -> [String: String] {
        let gate = ResumeGate<[String: String]>()
        return try await withCheckedThrowingContinuation { continuation in
            gate.set(continuation)
            let session = ASWebAuthenticationSession(url: url, callback: .customScheme("dev.reamde.substrate.chat")) { _, error in
                // The person closed the sheet, or the receiver's close below.
                gate.resume(.failure(error.map { ($0 as? ASWebAuthenticationSessionError)?.code == .canceledLogin ? Cancelled() : $0 }
                        ?? Cancelled()))
            }
            session.presentationContextProvider = self
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

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        #if os(macOS)
        NSApplication.shared.keyWindow ?? NSApplication.shared.windows.first ?? NSWindow()
        #else
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.flatMap(\.windows).first(where: \.isKeyWindow) ?? scenes.first.map { UIWindow(windowScene: $0) } ?? UIWindow()
        #endif
    }
}

/// Runs its block once, from whichever thread gets there first.
final class Once: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false

    func run(_ block: () -> Void) {
        lock.lock()
        defer { lock.unlock() }
        guard !done else { return }
        done = true
        block()
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
