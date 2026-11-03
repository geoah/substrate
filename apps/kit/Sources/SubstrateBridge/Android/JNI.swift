#if os(Android)
import Android
import Foundation
import SubstrateCore
import SubstrateJNI

// The whole JNI surface (docs/plans/app-kit.md §5.5): two natives in and one
// static Kotlin method out, all on `dev.reamde.substrate.kit.NativeBridge`.
//
//   NativeBridge.start(String): String   once, on the main thread
//   NativeBridge.post(String): String    every message from Kotlin
//   NativeBridge.deliver(String)         every message from Swift
//
// Every JNI call's failure is checked and becomes a BridgeFailure; nothing
// here is force-unwrapped, and no Kotlin exception is left pending.

typealias Env = UnsafeMutablePointer<JNIEnv?>
typealias VM = UnsafeMutablePointer<JavaVM?>

/// What JNI_OnLoad caches, read from any thread after: the class as a
/// global reference, since a thread Swift started sees only the system
/// class loader and `FindClass` there would not find it.
private struct Loaded: @unchecked Sendable {
    let vm: VM
    let bridgeClass: jclass
    let deliver: jmethodID
}

nonisolated(unsafe) private var loaded: Loaded?

/// Detaches a thread `attachedEnv` attached, when the thread exits: ART
/// aborts the process when a native thread exits still attached, and an
/// executor's threads exit when idle.
nonisolated(unsafe) private var detachKey = pthread_key_t()

private let bridgeClassName = "dev/reamde/substrate/kit/NativeBridge"

@_cdecl("JNI_OnLoad")
public func JNI_OnLoad(_ vm: UnsafeMutablePointer<JavaVM?>, _: UnsafeMutableRawPointer?) -> jint {
    guard let functions = vm.pointee?.pointee, let getEnv = functions.GetEnv else { return JNI_ERR }
    var raw: UnsafeMutableRawPointer?
    guard getEnv(vm, &raw, jint(JNI_VERSION_1_6)) == JNI_OK, let env = raw?.assumingMemoryBound(to: JNIEnv?.self),
          let jni = env.pointee?.pointee,
          let findClass = jni.FindClass, let newGlobalRef = jni.NewGlobalRef, let deleteLocalRef = jni.DeleteLocalRef,
          let getStaticMethodID = jni.GetStaticMethodID, let exceptionCheck = jni.ExceptionCheck,
          let exceptionClear = jni.ExceptionClear
    else { return JNI_ERR }
    guard let local = findClass(env, bridgeClassName) else {
        if exceptionCheck(env) != 0 { exceptionClear(env) }
        return JNI_ERR
    }
    defer { deleteLocalRef(env, local) }
    guard let global = newGlobalRef(env, local),
          let deliver = getStaticMethodID(env, global, "deliver", "(Ljava/lang/String;)V")
    else {
        if exceptionCheck(env) != 0 { exceptionClear(env) }
        return JNI_ERR
    }
    guard pthread_key_create(&detachKey, { _ in detachCurrentThread() }) == 0 else { return JNI_ERR }
    loaded = Loaded(vm: vm, bridgeClass: global, deliver: deliver)
    return jint(JNI_VERSION_1_6)
}

private func detachCurrentThread() {
    guard let vm = loaded?.vm, let detach = vm.pointee?.pointee.DetachCurrentThread else { return }
    _ = detach(vm)
}

@_cdecl("Java_dev_reamde_substrate_kit_NativeBridge_start")
public func Java_dev_reamde_substrate_kit_NativeBridge_start(_ env: UnsafeMutablePointer<JNIEnv?>, _: jclass, _ config: jstring?) -> jstring? {
    let answer: StartAnswer
    do {
        let text = try string(env, config)
        let decoded = try BridgeCodec.decode(StartConfig.self, from: JSONDecoder().decode(JSONValue.self, from: Data(text.utf8)))
        answer = BridgeRuntime.shared.start(decoded, outlet: KotlinOutlet(), log: androidLog) {
            if let failure = MainLooper.bind() { return failure }
            AppRoots.install(into: BridgeRuntime.shared.roots)
            return nil
        }
    } catch let failure as BridgeFailure {
        answer = .failed(failure: failure)
    } catch {
        answer = .failed(failure: BridgeFailure(.malformed, "the start config is not readable: \(BridgeCodec.describe(error))"))
    }
    let text = (try? String(decoding: BridgeCodec.encoder().encode(answer), as: UTF8.self))
        ?? #"{"failed":{"failure":{"code":"swiftFailure","message":"the start answer did not encode"}}}"#
    return try? newString(env, text)
}

@_cdecl("Java_dev_reamde_substrate_kit_NativeBridge_post")
public func Java_dev_reamde_substrate_kit_NativeBridge_post(_ env: UnsafeMutablePointer<JNIEnv?>, _: jclass, _ message: jstring?) -> jstring? {
    let reply: String
    do {
        reply = BridgeRuntime.shared.post(try string(env, message))
    } catch {
        reply = BridgeCodec.failure(error)
    }
    return try? newString(env, reply)
}

/// Kotlin's `deliver`, from whatever thread Swift is on.
struct KotlinOutlet: BridgeOutlet {
    func deliver(_ message: String) -> BridgeFailure? {
        guard let loaded else { return BridgeFailure(.jniFailure, "the library was not loaded through System.loadLibrary") }
        let env: Env
        do {
            env = try attachedEnv(loaded.vm)
        } catch {
            return error
        }
        guard let jni = env.pointee?.pointee, let pushLocalFrame = jni.PushLocalFrame, let popLocalFrame = jni.PopLocalFrame,
              let call = jni.CallStaticVoidMethodA
        else { return BridgeFailure(.jniFailure, "the JNI function table is incomplete") }
        // Each local reference made on an attached native thread lives until
        // it detaches; a stream would fill the table without a frame.
        guard pushLocalFrame(env, 4) == 0 else {
            clearException(env)
            return BridgeFailure(.jniFailure, "no local frame for a delivery")
        }
        defer { _ = popLocalFrame(env, nil) }
        let text: jstring
        do {
            text = try newString(env, message)
        } catch {
            return error
        }
        var arguments = [jvalue(l: text)]
        call(env, loaded.bridgeClass, loaded.deliver, &arguments)
        if clearException(env) {
            return BridgeFailure(.kotlinException, "NativeBridge.deliver threw")
        }
        return nil
    }
}

private func attachedEnv(_ vm: VM) throws(BridgeFailure) -> Env {
    guard let functions = vm.pointee?.pointee, let getEnv = functions.GetEnv, let attach = functions.AttachCurrentThread else {
        throw BridgeFailure(.jniFailure, "the JavaVM function table is incomplete")
    }
    var raw: UnsafeMutableRawPointer?
    if getEnv(vm, &raw, jint(JNI_VERSION_1_6)) == JNI_OK, let env = raw?.assumingMemoryBound(to: JNIEnv?.self) {
        return env
    }
    var env: Env?
    guard attach(vm, &env, nil) == JNI_OK, let env else {
        throw BridgeFailure(.jniFailure, "this thread could not attach to the VM")
    }
    // Any non-null value: the key's destructor runs only for one.
    pthread_setspecific(detachKey, UnsafeRawPointer(bitPattern: 1))
    return env
}

/// Clears a pending exception; true when there was one.
@discardableResult
private func clearException(_ env: Env) -> Bool {
    guard let jni = env.pointee?.pointee, let check = jni.ExceptionCheck, let clear = jni.ExceptionClear else { return false }
    guard check(env) != 0 else { return false }
    clear(env)
    return true
}

/// A Java string as Swift's, through UTF-16: JNI's "UTF" is modified UTF-8,
/// which writes a character outside the BMP as two surrogates.
private func string(_ env: Env, _ value: jstring?) throws(BridgeFailure) -> String {
    guard let value else { throw BridgeFailure(.malformed, "a null string") }
    guard let jni = env.pointee?.pointee, let length = jni.GetStringLength, let region = jni.GetStringRegion else {
        throw BridgeFailure(.jniFailure, "the JNI function table is incomplete")
    }
    let count = Int(length(env, value))
    let units = [UInt16](unsafeUninitializedCapacity: count) { buffer, initialized in
        region(env, value, 0, jsize(count), buffer.baseAddress)
        initialized = count
    }
    if clearException(env) { throw BridgeFailure(.jniFailure, "a string could not be read") }
    return String(decoding: units, as: UTF16.self)
}

private func newString(_ env: Env, _ value: String) throws(BridgeFailure) -> jstring {
    guard let jni = env.pointee?.pointee, let make = jni.NewString else {
        throw BridgeFailure(.jniFailure, "the JNI function table is incomplete")
    }
    let units = Array(value.utf16)
    let made = units.withUnsafeBufferPointer { make(env, $0.baseAddress, jsize($0.count)) }
    guard let made else {
        clearException(env)
        throw BridgeFailure(.jniFailure, "a string of \(units.count) UTF-16 units could not be made")
    }
    return made
}

/// Writes a recorded message, already redacted, to logcat.
private let androidLog: @Sendable (String) -> Void = { line in
    _ = __android_log_write(Int32(ANDROID_LOG_DEBUG.rawValue), "SubstrateBridge", line)
}
#endif
