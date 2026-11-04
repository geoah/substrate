package dev.reamde.substrate.kit

import android.content.Context
import android.content.pm.ApplicationInfo
import android.os.Build
import android.os.Looper
import android.util.Log
import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.BridgeStats
import dev.reamde.substrate.kit.bridge.ProtocolFingerprint
import dev.reamde.substrate.kit.bridge.StartAnswer
import dev.reamde.substrate.kit.bridge.StartConfig
import java.util.Locale
import java.util.TimeZone
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.serialization.json.JsonElement

/** What `Bridge.start` came to. A failure is terminal for the process. */
public sealed interface StartResult {
    public data class Started(val fingerprint: String) : StartResult

    public data class Failed(val failure: BridgeFailure) : StartResult
}

/**
 * The Swift core of the app, as Kotlin reaches it (docs/plans/app-kit.md §5.5). `start` once, from `Application.onCreate` on the main thread, so a background
 * job has it without an activity; then `open` a root by name.
 */
public object Bridge {
    private const val TAG = "SubstrateBridge"

    @Volatile private var core: BridgeCore? = null

    @Volatile private var result: StartResult? = null

    /**
     * Loads `library` (`System.loadLibrary`), copies the Swift resources out, and starts Swift with the device's locale, time zone and folders and the protocol
     * fingerprint this Kotlin was generated with. Nothing is half started: a failure leaves the bridge refusing every call.
     */
    @Synchronized
    public fun start(context: Context, services: DeviceServices = NoDeviceServices, library: String = "SubstrateAndroid"): StartResult {
        result?.let {
            return it
        }
        val app = context.applicationContext
        val debug = app.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        val log: ((String) -> Unit)? = if (debug) { line -> Log.d(TAG, line) } else null
        val started =
            begin(
                object : StartSteps {
                    override fun onMainThread() = Looper.myLooper() == Looper.getMainLooper()

                    override fun load() = System.loadLibrary(library)

                    override fun config() =
                        StartConfig(
                            fingerprint = ProtocolFingerprint.VALUE,
                            locale = Locale.getDefault().toLanguageTag(),
                            timeZone = TimeZone.getDefault().id,
                            resources = SwiftResources.folder(app).path,
                            data = app.noBackupFilesDir.path,
                            cache = app.cacheDir.path,
                            sdk = Build.VERSION.SDK_INT.toLong(),
                            debug = debug,
                        )

                    override fun native(config: String) = NativeBridge.start(config)

                    override fun started() {
                        val made = BridgeCore(NativeBridge::post, services, CoroutineScope(SupervisorJob() + Dispatchers.Default), log)
                        core = made
                        EnvironmentWatch.register(app) { update -> made.environment(update) }
                    }
                },
                library,
            )
        result = started
        return started
    }

    /** What `start` does that touches the process, apart, so a test runs `begin` without a device. */
    internal interface StartSteps {
        fun onMainThread(): Boolean

        fun load()

        fun config(): StartConfig

        fun native(config: String): String?

        fun started()
    }

    /**
     * Every step's failure is the terminal answer `start` latches, never a throw out of `Application.onCreate`: a library that will not load, a disk too full
     * to copy the resources out, an asset that will not read.
     */
    internal fun begin(steps: StartSteps, library: String): StartResult {
        if (!steps.onMainThread()) {
            return failed(BridgeFailure.Code.swiftFailure, "Bridge.start ran off the main thread; call it from Application.onCreate")
        }
        try {
            steps.load()
        } catch (failure: LinkageError) {
            return failed(BridgeFailure.Code.jniFailure, "lib$library.so did not load: ${failure.message}")
        } catch (failure: SecurityException) {
            return failed(BridgeFailure.Code.jniFailure, "lib$library.so did not load: ${failure.message}")
        }
        val config =
            try {
                steps.config()
            } catch (failure: Exception) {
                return failed(
                    BridgeFailure.Code.unavailable,
                    "the start's folders could not be made ready: ${failure::class.java.simpleName}: ${failure.message}",
                )
            }
        val answer =
            try {
                steps.native(BridgeJson.encodeToString(StartConfig.serializer(), config))
            } catch (failure: Exception) {
                return failed(BridgeFailure.Code.jniFailure, "start threw ${failure::class.java.simpleName}")
            } catch (failure: LinkageError) {
                return failed(BridgeFailure.Code.jniFailure, "start is not in lib$library.so: ${failure.message}")
            } ?: return failed(BridgeFailure.Code.jniFailure, "start answered nothing")
        val decoded =
            try {
                BridgeJson.decodeFromString(StartAnswer.Serializer, answer)
            } catch (_: Exception) {
                return failed(BridgeFailure.Code.malformed, "start answered something Kotlin cannot read")
            }
        return when (decoded) {
            is StartAnswer.Started ->
                try {
                    steps.started()
                    StartResult.Started(decoded.fingerprint)
                } catch (failure: Exception) {
                    failed(BridgeFailure.Code.kotlinException, "the bridge's Kotlin did not start: ${failure::class.java.simpleName}: ${failure.message}")
                }
            is StartAnswer.Failed -> StartResult.Failed(decoded.failure)
        }
    }

    private fun failed(code: BridgeFailure.Code, message: String) = StartResult.Failed(BridgeFailure(code, message))

    private fun started(): BridgeCore =
        core ?: throw BridgeException((result as? StartResult.Failed)?.failure ?: BridgeFailure(BridgeFailure.Code.notStarted, "Bridge.start has not run"))

    public fun open(root: String, settings: JsonElement? = null): Result<BridgeHandle> = runCatching {
        started()
    }
        .fold({ it.open(root, settings) }, { Result.failure(it) })

    public suspend fun stats(): BridgeStats = started().stats()

    /** Calls Swift asked for that are still running on this side. */
    public val runningCalls: Int
        get() = core?.runningCalls ?: 0

    internal fun receive(message: String) {
        core?.receive(message)
    }

    internal fun recordFailure(what: String) {
        core?.recordFailure(what) ?: Log.w(TAG, what)
    }
}
