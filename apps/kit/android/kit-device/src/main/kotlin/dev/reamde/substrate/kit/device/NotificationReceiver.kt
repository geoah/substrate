package dev.reamde.substrate.kit.device

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import dev.reamde.substrate.kit.bridge.NotificationResponse

/**
 * What Android hands `Notifications` while the app may not be running: an alarm that came due, an action answered in the background, a dismissal the category
 * reports, and a reboot, after which every pending alarm is set again.
 */
public class NotificationReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val notifications = Notifications.get(context)
        val id = intent.getStringExtra(Notifications.EXTRA_ID)
        when (intent.action) {
            Intent.ACTION_BOOT_COMPLETED,
            Intent.ACTION_MY_PACKAGE_REPLACED -> notifications.rearm()
            Notifications.ACTION_FIRE -> id?.let(notifications::fire)
            Notifications.ACTION_DISMISS -> id?.let { notifications.respond(intent, it, NotificationResponse.Action.Dismiss) }
            Notifications.ACTION_ACTION ->
                id?.let { found ->
                    val action = intent.getStringExtra(Notifications.EXTRA_ACTION) ?: return
                    notifications.respond(intent, found, NotificationResponse.Action.Named(action))
                }
        }
    }
}
