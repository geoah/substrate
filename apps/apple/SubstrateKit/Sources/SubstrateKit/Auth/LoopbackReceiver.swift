import Foundation

/// Receives the authorization redirect on a loopback port (RFC 8252 §7.3).
/// A native app has no web host to claim an https redirect with, and the
/// server admits `http` on a loopback host at any port, so the app listens on
/// 127.0.0.1 while the sign-in sheet is up and the browser lands here. A
/// plain socket, not Network.framework's listener: on iOS that listener
/// accepts at the kernel and never hands the connection over, under the
/// local network policy.
public final class LoopbackReceiver: @unchecked Sendable {
    private let socket: Int32
    private let source: DispatchSourceRead
    private let lock = NSLock()
    private var waiting: CheckedContinuation<[String: String], Error>?
    private var received: Result<[String: String], Error>?
    private let appName: String
    public let path: String
    public let port: UInt16

    public var redirect: String { "http://127.0.0.1:\(port)\(path)" }

    public struct Unavailable: LocalizedError, Sendable {
        public let appName: String
        public let what: String
        public var errorDescription: String? { "\(appName) couldn't listen for the sign-in to come back (\(what))." }
    }

    private init(socket: Int32, port: UInt16, appName: String, path: String) {
        self.socket = socket
        self.port = port
        self.appName = appName
        self.path = path
        let slug = appName.lowercased().filter { $0.isASCII && ($0.isLetter || $0.isNumber) }
        source = DispatchSource.makeReadSource(fileDescriptor: socket,
                                               queue: DispatchQueue(label: "dev.reamde.substrate.\(slug).oauth"))
    }

    /// Listens on a free loopback port. `appName` is what the page the
    /// browser lands on tells the person to go back to.
    public static func start(appName: String, path: String = "/oauth/callback") async throws -> LoopbackReceiver {
        let fd = Darwin.socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { throw Unavailable(appName: appName, what: "socket \(errno)") }
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
            throw Unavailable(appName: appName, what: "bind \(errno)")
        }
        var length = socklen_t(MemoryLayout<sockaddr_in>.size)
        _ = withUnsafeMutablePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(fd, $0, &length) }
        }
        _ = fcntl(fd, F_SETFL, O_NONBLOCK)
        let receiver = LoopbackReceiver(socket: fd, port: UInt16(bigEndian: address.sin_port), appName: appName, path: path)
        receiver.source.setEventHandler { [weak receiver] in receiver?.accept() }
        receiver.source.setCancelHandler { close(fd) }
        receiver.source.resume()
        return receiver
    }

    /// The redirect's query parameters, once the browser arrives.
    public func next() async throws -> [String: String] {
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

    public func stop() {
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
        guard let components = URLComponents(string: "http://127.0.0.1\(target)"), components.path == path else {
            write(client, status: "404 Not Found", body: "Not found.")
            close(client)
            return
        }
        var query: [String: String] = [:]
        for item in components.queryItems ?? [] { query[item.name] = item.value ?? "" }
        write(client, status: "200 OK", body: query["error"] != nil
            ? "Access was not granted. You can go back to \(appName)."
            : "Signed in. You can go back to \(appName).")
        close(client)
        finish(.success(query))
    }

    private func write(_ client: Int32, status: String, body: String) {
        let page = """
        <!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">\
        <title>\(Self.escape(appName))</title></head><body style="font:17px -apple-system,system-ui;\
        display:flex;align-items:center;justify-content:center;height:90vh;color:#444">\(Self.escape(body))</body></html>
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

    private static func escape(_ text: String) -> String {
        text.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
    }
}
