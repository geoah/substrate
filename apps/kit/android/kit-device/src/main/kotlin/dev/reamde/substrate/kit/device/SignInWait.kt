package dev.reamde.substrate.kit.device

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log

/**
 * Keeps the app's process running while the owner is in their browser: a process in the background is frozen within seconds (Android 14 and later freeze cached
 * apps), and a frozen process's loopback listener takes no callback; one the system kills loses the attempt a claimed callback would end. A short foreground
 * service, started while the app is still in front and stopped when the sign-in finishes, says so in a notification. Android ends a short service after about
 * three minutes; a sign-in left that long is answered when the app comes back to the front.
 */
public class SignInWait : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val manager = getSystemService(NotificationManager::class.java)
        if (manager.getNotificationChannel(CHANNEL) == null) {
            manager.createNotificationChannel(NotificationChannel(CHANNEL, "Signing in", NotificationManager.IMPORTANCE_LOW))
        }
        val label = applicationInfo.loadLabel(packageManager)
        val notification =
            Notification.Builder(this, CHANNEL)
                .setSmallIcon(R.drawable.substrate_notification)
                .setContentTitle("Waiting for your browser")
                .setContentText("$label finishes signing in when you approve it there.")
                .setOngoing(true)
                .build()
        try {
            if (Build.VERSION.SDK_INT >= 34) {
                startForeground(NOTIFICATION, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SHORT_SERVICE)
            } else {
                startForeground(NOTIFICATION, notification)
            }
        } catch (failure: RuntimeException) {
            // Not allowed to start now (the app already left the front): the
            // sign-in still completes when the app comes back.
            Log.w(TAG, "could not hold the process for the sign-in: ${failure.message}")
            stopSelf()
        }
        return START_NOT_STICKY
    }

    override fun onTimeout(startId: Int) {
        stopSelf()
    }

    internal companion object {
        private const val TAG = "SubstrateSignIn"
        private const val CHANNEL = "substrate.signin"
        private const val NOTIFICATION = 2

        fun start(context: Context) {
            runCatching { context.startForegroundService(Intent(context, SignInWait::class.java)) }
                .onFailure { Log.w(TAG, "could not hold the process for the sign-in: ${it.message}") }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, SignInWait::class.java))
        }
    }
}
