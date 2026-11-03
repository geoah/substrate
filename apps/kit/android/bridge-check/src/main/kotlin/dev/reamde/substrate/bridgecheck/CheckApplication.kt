package dev.reamde.substrate.bridgecheck

import android.app.Application
import android.os.SystemClock
import dev.reamde.substrate.kit.Bridge
import dev.reamde.substrate.kit.StartResult
import dev.reamde.substrate.kit.device.KitDevice

/** Starts the bridge where every app does: Application.onCreate, on the main thread. */
class CheckApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        val began = SystemClock.elapsedRealtimeNanos()
        start = Bridge.start(this, KitDevice.services(this))
        startMillis = (SystemClock.elapsedRealtimeNanos() - began) / 1e6
    }

    companion object {
        @Volatile var start: StartResult? = null
            private set

        /** How long `Bridge.start` took, loading the libraries included. */
        @Volatile var startMillis: Double = 0.0
            private set
    }
}
