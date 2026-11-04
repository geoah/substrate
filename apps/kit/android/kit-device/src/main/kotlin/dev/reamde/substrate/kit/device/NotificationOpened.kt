package dev.reamde.substrate.kit.device

import android.app.Activity
import android.content.Intent
import android.os.Bundle

/**
 * Where a notification's tap, and an action that opens the app, land: an activity no other app may start (`exported="false"`), so the response it hands
 * `Notifications` comes from a `PendingIntent` this app made and never from an intent someone else built. It brings the app's task to the front as it stood,
 * the way `ReturnToApp` does, and is gone.
 */
public class NotificationOpened : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) Notifications.get(this).handle(intent)
        packageManager.getLaunchIntentForPackage(packageName)?.let { launch ->
            runCatching { startActivity(launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT)) }
        }
        finish()
    }
}
