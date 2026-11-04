package dev.reamde.substrate.kit.device

import android.app.Activity
import android.content.Intent
import android.os.Bundle

/**
 * Where the sign-in's last page sends the owner back to the app: the
 * browser adds BROWSABLE to every intent a page starts, and an app's
 * launcher activity takes none, so this one does, on a scheme made of the
 * application id (`<applicationId>.signin`). It reads nothing from the
 * intent and starts nothing but the app's own launcher activity, which
 * brings the app's task to the front as it stood.
 */
public class ReturnToApp : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        packageManager.getLaunchIntentForPackage(packageName)?.let { launch ->
            startActivity(launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT))
        }
        finish()
    }

    internal companion object {
        /** The page's link to this activity, for the app [packageName]. */
        fun link(packageName: String): String = "intent://return#Intent;scheme=$packageName.signin;package=$packageName;end"
    }
}
