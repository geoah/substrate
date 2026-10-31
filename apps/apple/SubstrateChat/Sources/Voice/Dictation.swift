@preconcurrency import AVFoundation
import Foundation
import Observation
import Speech

/// Voice typing: the microphone through an `AVAudioEngine` tap, converted to
/// the analyzer's format, into `SpeechAnalyzer` with a `SpeechTranscriber`
/// (on device), or a `DictationTranscriber` where the transcriber is not
/// available. Finalized words and the volatile guess after them are kept
/// apart, so the composer can draw the guess dimmed.
@MainActor
@Observable
final class Dictation {
    enum State: Equatable {
        case idle
        /// Asking for access, or downloading the speech model.
        case preparing(String)
        case listening
        case finishing
    }

    private(set) var state = State.idle
    private(set) var finalized = ""
    private(set) var volatile = ""
    /// The input's loudness, 0 to 1, for the meter.
    private(set) var level: Float = 0
    /// Why listening stopped or never started, in a sentence.
    var problem: String?

    @ObservationIgnored private var engine: AVAudioEngine?
    @ObservationIgnored private var analyzer: SpeechAnalyzer?
    @ObservationIgnored private var input: AsyncStream<AnalyzerInput>.Continuation?
    @ObservationIgnored private var results: Task<Void, Never>?
    @ObservationIgnored private var interruption: NSObjectProtocol?
    @ObservationIgnored private var generation = 0

    var active: Bool { state != .idle }
    var heard: String { finalized + volatile }

    /// Starts listening in `localeID`'s language, else the device's.
    func start(localeID: String?) async {
        guard state == .idle else { return }
        generation += 1
        let generation = generation
        problem = nil
        finalized = ""
        volatile = ""
        state = .preparing("Starting…")
        do {
            guard await Self.microphoneAllowed() else {
                throw Problem(text: "Substrate Chat may not use the microphone. Allow it in Settings → Privacy & Security → Microphone.")
            }
            let wanted = localeID.map(Locale.init(identifier:)) ?? Locale.current
            let module = try await Self.module(for: wanted)
            guard generation == self.generation else { return }
            if let request = try await AssetInventory.assetInstallationRequest(supporting: [module.speech]) {
                state = .preparing("Downloading the speech model…")
                try await request.downloadAndInstall()
            }
            guard generation == self.generation else { return }
            try configureSession()
            guard let format = await SpeechAnalyzer.bestAvailableAudioFormat(compatibleWith: [module.speech]) else {
                throw Problem(text: "This device has no audio format the speech model reads.")
            }
            let (stream, continuation) = AsyncStream<AnalyzerInput>.makeStream()
            let analyzer = SpeechAnalyzer(modules: [module.speech])
            let engine = AVAudioEngine()
            let node = engine.inputNode
            let natural = node.outputFormat(forBus: 0)
            guard natural.sampleRate > 0 else { throw Problem(text: "No microphone is available.") }
            let converter = natural == format ? nil : AVAudioConverter(from: natural, to: format)
            node.installTap(onBus: 0, bufferSize: 2048, format: natural) { [weak self] buffer, _ in
                let level = Self.level(buffer)
                if let converted = Self.convert(buffer, with: converter, to: format) {
                    continuation.yield(AnalyzerInput(buffer: converted))
                }
                Task { @MainActor in self?.level = level }
            }
            engine.prepare()
            try engine.start()
            try await analyzer.start(inputSequence: stream)
            self.engine = engine
            self.analyzer = analyzer
            input = continuation
            results = Task { [weak self] in
                do {
                    for try await (text, final) in module.results {
                        guard let self else { return }
                        if final {
                            self.finalized += text
                            self.volatile = ""
                        } else {
                            self.volatile = text
                        }
                    }
                } catch {}
            }
            observeInterruptions()
            state = .listening
        } catch {
            await teardown()
            problem = (error as? Problem)?.text ?? "Couldn't start listening: \(error.localizedDescription)"
            state = .idle
        }
    }

    /// Stops listening and answers everything heard, finalized within a
    /// second and a half; what is still a guess then is kept as heard.
    @discardableResult
    func stop() async -> String {
        guard state == .listening || state != .idle else { return heard }
        generation += 1
        state = .finishing
        engine?.inputNode.removeTap(onBus: 0)
        engine?.stop()
        input?.finish()
        if let analyzer {
            let finishing = Task { try? await analyzer.finalizeAndFinishThroughEndOfInput() }
            let deadline = Task {
                try? await Task.sleep(for: .milliseconds(1500))
                await analyzer.cancelAndFinishNow()
            }
            await finishing.value
            deadline.cancel()
        }
        _ = await withTaskGroup(of: Void.self) { group in
            if let results { group.addTask { await results.value } }
            group.addTask { try? await Task.sleep(for: .milliseconds(400)) }
            await group.next()
            group.cancelAll()
        }
        let text = heard
        await teardown()
        state = .idle
        return text
    }

    /// Stops without waiting for the last words.
    func cancel() {
        generation += 1
        Task {
            await analyzer?.cancelAndFinishNow()
            await teardown()
            state = .idle
        }
    }

    private func teardown() async {
        engine?.inputNode.removeTap(onBus: 0)
        engine?.stop()
        engine = nil
        input?.finish()
        input = nil
        results?.cancel()
        results = nil
        analyzer = nil
        level = 0
        if let interruption { NotificationCenter.default.removeObserver(interruption) }
        interruption = nil
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        #endif
    }

    private func configureSession() throws {
        #if os(iOS)
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .spokenAudio, options: [.duckOthers, .defaultToSpeaker, .allowBluetoothHFP])
        try session.setActive(true, options: .notifyOthersOnDeactivation)
        #endif
    }

    /// A call, Siri or another app taking the input stops listening and
    /// keeps what was heard.
    private func observeInterruptions() {
        #if os(iOS)
        interruption = NotificationCenter.default.addObserver(
            forName: AVAudioSession.interruptionNotification, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                guard let self, self.state == .listening else { return }
                await self.stop()
            }
        }
        #endif
    }

    // MARK: The model

    private struct Module {
        let speech: any SpeechModule
        let results: AsyncThrowingStream<(String, Bool), Error>
    }

    private struct Problem: LocalizedError {
        let text: String
        var errorDescription: String? { text }
    }

    private static func module(for wanted: Locale) async throws -> Module {
        if SpeechTranscriber.isAvailable,
           let locale = await supported(wanted, all: { await SpeechTranscriber.supportedLocales },
                                        equivalent: { await SpeechTranscriber.supportedLocale(equivalentTo: $0) }) {
            let transcriber = SpeechTranscriber(locale: locale, transcriptionOptions: [],
                                                reportingOptions: [.volatileResults], attributeOptions: [])
            return Module(speech: transcriber, results: map(transcriber.results) { (String($0.text.characters), $0.isFinal) })
        }
        if let locale = await supported(wanted, all: { await DictationTranscriber.supportedLocales },
                                        equivalent: { await DictationTranscriber.supportedLocale(equivalentTo: $0) }) {
            let transcriber = DictationTranscriber(locale: locale, preset: .progressiveLongDictation)
            return Module(speech: transcriber, results: map(transcriber.results) { (String($0.text.characters), $0.isFinal) })
        }
        throw Problem(text: "Speech recognition doesn't support \(wanted.localizedString(forIdentifier: wanted.identifier) ?? wanted.identifier) on this device. Pick another language in Settings → Voice.")
    }

    /// The locale itself, else one of the same language.
    private static func supported(_ wanted: Locale, all: () async -> [Locale],
                                  equivalent: (Locale) async -> Locale?) async -> Locale? {
        if let match = await equivalent(wanted) { return match }
        let language = wanted.language.languageCode
        return await all().first { $0.language.languageCode == language }
    }

    private static func map<S: AsyncSequence & Sendable>(
        _ sequence: S, _ transform: @escaping @Sendable (S.Element) -> (String, Bool)
    ) -> AsyncThrowingStream<(String, Bool), Error> where S.Element: Sendable {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    for try await element in sequence { continuation.yield(transform(element)) }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    static func microphoneAllowed() async -> Bool {
        await withCheckedContinuation { continuation in
            AVAudioApplication.requestRecordPermission { continuation.resume(returning: $0) }
        }
    }

    // MARK: Audio, on the tap's thread

    nonisolated private static func level(_ buffer: AVAudioPCMBuffer) -> Float {
        guard let data = buffer.floatChannelData?[0], buffer.frameLength > 0 else { return 0 }
        var sum: Float = 0
        let n = Int(buffer.frameLength)
        for i in 0 ..< n { sum += data[i] * data[i] }
        let rms = (sum / Float(n)).squareRoot()
        // About -50 dB is silence and -10 dB loud speech.
        let db = 20 * log10(max(rms, 0.000_01))
        return max(0, min(1, (db + 50) / 40))
    }

    nonisolated private static func convert(_ buffer: AVAudioPCMBuffer, with converter: AVAudioConverter?,
                                            to format: AVAudioFormat) -> AVAudioPCMBuffer? {
        guard let converter else { return buffer }
        let ratio = format.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount((Double(buffer.frameLength) * ratio).rounded(.up)) + 16
        guard let out = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: capacity) else { return nil }
        nonisolated(unsafe) var consumed = false
        var error: NSError?
        converter.convert(to: out, error: &error) { _, status in
            if consumed {
                status.pointee = .noDataNow
                return nil
            }
            consumed = true
            status.pointee = .haveData
            return buffer
        }
        return error == nil && out.frameLength > 0 ? out : nil
    }
}
