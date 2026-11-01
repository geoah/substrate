import Foundation

/// A request's body.
public enum RequestBody: Sendable {
    case none
    /// JSON bytes, as written: an app with byte-exact payloads encodes its
    /// own.
    case json(Data)
    case bytes(Data, contentType: String)
    /// `application/x-www-form-urlencoded`, each pair encoded once.
    case form([(String, String)])

    /// A value as JSON: a whole number as an integer, since the server reads
    /// a version or a count into one, and slashes unescaped.
    public static func jsonValue(_ value: JSONValue) throws -> RequestBody {
        .json(try JSONSerialization.data(withJSONObject: value.any, options: [.withoutEscapingSlashes, .fragmentsAllowed]))
    }

    /// An encodable value as JSON, slashes unescaped.
    public static func encoding(_ value: some Encodable) throws -> RequestBody {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        return .json(try encoder.encode(value))
    }
}

/// The substrate API as every app speaks it: one server, one bearer token,
/// over an injected transport. Requests name no actor, so the server records
/// a password session's writes as `api` and an app token's as `app:<host>`.
public struct SubstrateClient: Sendable {
    public var server: URL
    public var token: String?
    public var transport: any HTTPTransport
    /// How a retry waits; a test replaces it so nothing sleeps.
    public var pause: @Sendable (TimeInterval) async throws -> Void = { try await Task.sleep(for: .seconds($0)) }

    public init(server: URL, token: String? = nil, transport: any HTTPTransport = URLSessionTransport.shared) {
        self.server = server
        self.token = token
        self.transport = transport
    }

    public init(_ session: Session, transport: any HTTPTransport = URLSessionTransport.shared) {
        self.init(server: session.server, token: session.secret, transport: transport)
    }

    /// How long a call may take, by what it asks of the server. A read the
    /// server answers from its index is quick or something is wrong; an
    /// install does real work; an upload is bounded by the device's uplink.
    public enum Timeout {
        public static let quick: TimeInterval = 20
        public static let read: TimeInterval = 30
        public static let upload: TimeInterval = 120
        public static let serverWork: TimeInterval = 300
    }

    /// One answer: its body, its status and its headers.
    public struct Response: Sendable {
        public var data: Data
        public var status: Int
        public var headers: [String: String]

        init(data: Data, status: Int, headers: [String: String]) {
            self.data = data
            self.status = status
            self.headers = headers
        }

        /// An answer a caller read itself, such as a stream's.
        public init(data: Data, response: HTTPURLResponse) {
            self.init(data: data, status: response.statusCode, headers: SubstrateClient.headers(of: response))
        }

        public func header(_ name: String) -> String? {
            headers.first { $0.key.caseInsensitiveCompare(name) == .orderedSame }?.value
        }

        public func decode<T: Decodable>(_ type: T.Type) throws -> T {
            try JSONDecoder().decode(type, from: data)
        }
    }

    // MARK: Building requests

    /// A path under the server, joined as text so a segment already
    /// percent-encoded is sent as it is: `URL.appending(path:)` encodes `%`
    /// again, and an id with a slash would reach the server as `%252F`.
    public func url(_ path: String, query: [(String, String)] = []) throws -> URL {
        var text = server.absoluteString.trimmingTrailingSlashes + "/" + path.drop { $0 == "/" }
        if !query.isEmpty {
            text += "?" + query.map { "\(segment($0.0))=\(segment($0.1))" }.joined(separator: "&")
        }
        guard let url = URL(string: text) else { throw URLError(.badURL) }
        return url
    }

    public func request(_ method: String, _ path: String, query: [(String, String)] = [], body: RequestBody = .none,
                        accept: String = "application/json", timeout: TimeInterval = Timeout.read,
                        authorized: Bool = true) throws -> URLRequest
    {
        var request = URLRequest(url: try url(path, query: query))
        request.httpMethod = method
        request.timeoutInterval = timeout
        request.setValue(accept, forHTTPHeaderField: "Accept")
        if authorized, let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        switch body {
        case .none: break
        case .json(let data):
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = data
        case .bytes(let data, let contentType):
            request.setValue(contentType, forHTTPHeaderField: "Content-Type")
            request.httpBody = data
        case .form(let pairs):
            request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
            request.httpBody = Data(pairs.map { "\(segment($0.0))=\(segment($0.1))" }.joined(separator: "&").utf8)
        }
        return request
    }

    // MARK: Sending

    /// Sends a request and answers on a 2xx or a status in `accept`;
    /// anything else throws (`failure(_:)`). A busy server's 429 or 503 is
    /// waited out, as its `Retry-After` says, `retries` times; by default
    /// only a request that may run twice (GET, HEAD, PUT, DELETE) is sent
    /// again, since a POST or a PATCH that timed out may have landed.
    @discardableResult
    public func send(_ request: URLRequest, accept: Set<Int> = [], retries: Int? = nil,
                     longestWait: TimeInterval = 10) async throws -> Response
    {
        let method = request.httpMethod?.uppercased() ?? "GET"
        let limit = retries ?? (Self.idempotent.contains(method) ? 3 : 0)
        var attempt = 0
        while true {
            let (data, http) = try await transport.data(for: request)
            let response = Response(data: data, status: http.statusCode, headers: Self.headers(of: http))
            if (200 ..< 300).contains(response.status) || accept.contains(response.status) { return response }
            if response.status == 429 || response.status == 503, attempt < limit {
                attempt += 1
                try await pause(Self.wait(after: response, attempt: attempt, longest: longestWait))
                continue
            }
            throw Self.failure(response)
        }
    }

    /// Builds, sends and answers in one step.
    @discardableResult
    public func send(_ method: String, _ path: String, query: [(String, String)] = [], body: RequestBody = .none,
                     accept: Set<Int> = [], timeout: TimeInterval = Timeout.read, retries: Int? = nil) async throws -> Response
    {
        try await send(request(method, path, query: query, body: body, timeout: timeout), accept: accept, retries: retries)
    }

    static let idempotent: Set<String> = ["GET", "HEAD", "PUT", "DELETE", "OPTIONS"]

    /// A refusal as the error that says what it means: a 401 is the sign-in
    /// ending, a 403 naming `insufficient_scope` an approval that does not
    /// reach, the envelope the server's own words, anything else not the
    /// server's answer.
    public static func failure(_ response: Response) -> Error {
        let envelope = ServerError(status: response.status, envelope: response.data)
        if response.status == 401 { return SessionExpired(message: envelope?.text) }
        if response.status == 403, response.header("WWW-Authenticate")?.contains("insufficient_scope") == true {
            return InsufficientScope(message: envelope?.text)
        }
        return envelope ?? TransportError(status: response.status, body: response.data)
    }

    /// How long to wait before the next try: the server's `Retry-After`
    /// (seconds or a date), else a doubling backoff, never past `longest`.
    static func wait(after response: Response, attempt: Int, longest: TimeInterval) -> TimeInterval {
        let backoff = min(pow(2, Double(attempt - 1)), longest)
        guard let header = response.header("Retry-After")?.trimmingCharacters(in: .whitespaces) else { return backoff }
        if let seconds = Double(header) { return min(max(seconds, 0), longest) }
        let format = DateFormatter()
        format.locale = Locale(identifier: "en_US_POSIX")
        format.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
        if let date = format.date(from: header) { return min(max(date.timeIntervalSinceNow, 0), longest) }
        return backoff
    }

    static func headers(of response: HTTPURLResponse) -> [String: String] {
        var out: [String: String] = [:]
        for (key, value) in response.allHeaderFields {
            if let key = key as? String, let value = value as? String { out[key] = value }
        }
        return out
    }
}

/// One path segment, encoded once: everything outside RFC 3986's ASCII
/// unreserved set is escaped, `/` included, since a record id may carry one
/// (an agent's id is `<authority>/<package>/<name>`). The one encoder every
/// path and query in this kit goes through.
public func segment(_ text: String) -> String {
    text.addingPercentEncoding(withAllowedCharacters: unreserved) ?? text
}

private let unreserved = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~")
