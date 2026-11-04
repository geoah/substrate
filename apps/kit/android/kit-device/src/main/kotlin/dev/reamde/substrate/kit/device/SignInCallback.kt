package dev.reamde.substrate.kit.device

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.util.Log

/**
 * Where a release's sign-in comes back: the https callback the app claims, which Android hands this app alone once the system has verified the site's
 * `assetlinks.json` names it (an App Link). Only a release's manifest declares it, for the stamped callback's host and path (build-logic's
 * `requireReleaseSignIn`); a debug build signs in through loopback. It hands the address to the attempt waiting on it, which `BrowserSignIn.claimed` matches by
 * the `state` it carries, and brings the app's task forward as it stood. An address no attempt waits on ends nothing and starts nothing.
 */
public class SignInCallback : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Made again from a saved state, it hands nothing a second time.
        if (savedInstanceState == null) {
            val delivered = intent?.dataString?.let(BrowserSignIn.claimed::deliver)
            if (delivered is ClaimedCallbacks.Delivered.Refused) Log.w(TAG, "a sign-in callback ended nothing: ${delivered.reason}")
        }
        packageManager.getLaunchIntentForPackage(packageName)?.let { launch ->
            startActivity(launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT))
        }
        finish()
    }

    private companion object {
        const val TAG = "SubstrateSignIn"
    }
}
