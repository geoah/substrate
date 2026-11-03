package dev.reamde.substrate.kit.device

import android.app.Activity
import android.app.Application
import android.os.Bundle
import java.lang.ref.WeakReference

/**
 * The app's activity in front, if any: what a service that must show the
 * system's own screen (a permission's question, the browser) starts it
 * from, and whether a notification is shown over the app or beside it.
 */
internal object ForegroundActivity : Application.ActivityLifecycleCallbacks {
    @Volatile private var resumed: WeakReference<Activity>? = null

    @Volatile private var registered = false

    fun watch(app: Application) {
        synchronized(this) {
            if (registered) return
            registered = true
        }
        app.registerActivityLifecycleCallbacks(this)
    }

    val current: Activity? get() = resumed?.get()?.takeUnless { it.isFinishing || it.isDestroyed }

    override fun onActivityResumed(activity: Activity) {
        resumed = WeakReference(activity)
    }

    override fun onActivityPaused(activity: Activity) {
        if (resumed?.get() === activity) resumed = null
    }

    override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = Unit

    override fun onActivityStarted(activity: Activity) = Unit

    override fun onActivityStopped(activity: Activity) = Unit

    override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = Unit

    override fun onActivityDestroyed(activity: Activity) = Unit
}
