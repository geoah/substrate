package dev.reamde.substrate.kit.gallery

import android.app.Application
import android.util.Log
import dev.reamde.substrate.kit.Bridge
import dev.reamde.substrate.kit.StartResult
import dev.reamde.substrate.kit.device.KitDevice

/** Starts the bridge where every app does: Application.onCreate, on the main thread. */
class GalleryApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        start = Bridge.start(this, KitDevice.services(this))
        Log.i("gallery", "start: $start")
    }

    companion object {
        @Volatile
        var start: StartResult? = null
            private set
    }
}
