package dev.reamde.substrate.kit

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Build
import dev.reamde.substrate.kit.bridge.EnvironmentUpdate
import java.util.Locale
import java.util.TimeZone

/**
 * The device's locale and time zone as they change while the process runs: `start` hands Swift the ones at launch, and each broadcast after it posts the new
 * pair (`environment`), which every open root hears in order with its commands. The resource folder never changes in a process: it is copied out once per
 * install, and an update starts a new process.
 */
internal object EnvironmentWatch {
    private val actions = listOf(Intent.ACTION_LOCALE_CHANGED, Intent.ACTION_TIMEZONE_CHANGED)

    fun register(context: Context, post: (EnvironmentUpdate) -> Unit) {
        val receiver =
            object : BroadcastReceiver() {
                override fun onReceive(context: Context, intent: Intent) {
                    if (intent.action in actions) post(current())
                }
            }
        val filter = IntentFilter().apply { actions.forEach(::addAction) }
        // Both are protected broadcasts only the system sends, which a
        // receiver no other app may reach still gets.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            context.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            context.registerReceiver(receiver, filter)
        }
    }

    /** The pair now. `Locale.getDefault()` follows the device's on a locale change. */
    fun current(): EnvironmentUpdate = EnvironmentUpdate(Locale.getDefault().toLanguageTag(), TimeZone.getDefault().id)
}
