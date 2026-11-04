package dev.reamde.substrate.kit.device

import android.Manifest
import android.app.Activity
import android.content.pm.PackageManager
import android.os.Bundle

/**
 * The system's question for the notification permission, asked from an activity of its own with nothing drawn, so `Notifications` asks it from whatever
 * activity is in front without that activity's help.
 */
public class PermissionActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (savedInstanceState == null) requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        Notifications.get(this).answered(grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED)
        finish()
    }

    override fun onDestroy() {
        // Closed before it answered (a configuration change, the back key):
        // the question is over either way.
        if (isFinishing) Notifications.get(this).answered(false)
        super.onDestroy()
    }

    private companion object {
        const val REQUEST = 1
    }
}
