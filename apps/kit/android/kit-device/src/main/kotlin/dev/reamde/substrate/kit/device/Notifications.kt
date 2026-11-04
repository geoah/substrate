package dev.reamde.substrate.kit.device

import android.Manifest
import android.app.AlarmManager
import android.app.Application
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.RemoteInput
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.util.Log
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.NotificationCategory
import dev.reamde.substrate.kit.bridge.NotificationPermission
import dev.reamde.substrate.kit.bridge.NotificationRequest
import dev.reamde.substrate.kit.bridge.NotificationResponse
import java.io.File
import java.time.Instant
import java.time.OffsetDateTime
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.serialization.builtins.ListSerializer

/**
 * The device's local notifications (SubstrateDevice's `Notifications`,
 * docs/plans/app-kit.md §5.4) on `NotificationManager`. A request with a
 * `fireAt` is kept in `noBackupFilesDir/notifications` until it is shown,
 * with an alarm to show it then, armed again at every start and after a
 * reboot, so `pending(prefix)` answers across launches; one with none is
 * shown at once, and one whose instant has passed is dropped. The owner's
 * tap, action or dismissal is a `NotificationResponse` on [responses], each
 * once, kept for the first reader. A tap lands on `NotificationOpened`,
 * which no other app may start, never on the app's exported launcher
 * activity, so no response is made from extras another app wrote. Nothing
 * here is backed up, so a restore never revives a
 * reminder for a session that is gone.
 *
 * The alarm is exact where the owner lets the app schedule exact alarms,
 * and otherwise as close as Android's idle rules allow.
 */
public class Notifications internal constructor(context: Context, private val channelName: String) {
    private val app = context.applicationContext
    private val manager = app.getSystemService(NotificationManager::class.java)
    private val alarms = app.getSystemService(AlarmManager::class.java)
    private val folder = File(app.noBackupFilesDir, "notifications")
    private val lock = Any()
    private val answers = Channel<NotificationResponse>(Channel.UNLIMITED)

    @Volatile private var asking: CompletableDeferred<Boolean>? = null

    init {
        (app as? Application)?.let(ForegroundActivity::watch)
        rearm()
    }

    // --- permission ----------------------------------------------------------

    public fun permission(): NotificationPermission {
        val enabled = manager.areNotificationsEnabled()
        if (Build.VERSION.SDK_INT < 33) return if (enabled) NotificationPermission.on else NotificationPermission.off
        val granted = app.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
        return when {
            granted && enabled -> NotificationPermission.on
            !granted && !File(folder, ASKED).exists() -> NotificationPermission.notAsked
            else -> NotificationPermission.off
        }
    }

    /**
     * Asks once, from the activity in front, where the owner was never
     * asked; answers where the permission stands after. With no activity
     * in front there is nobody to ask, and nothing is marked asked.
     */
    public suspend fun requestPermission(): NotificationPermission {
        if (permission() != NotificationPermission.notAsked) return permission()
        val from = ForegroundActivity.current ?: return permission()
        val answer = CompletableDeferred<Boolean>()
        asking = answer
        from.startActivity(Intent(from, PermissionActivity::class.java))
        answer.await()
        folder.mkdirs()
        File(folder, ASKED).writeText("")
        return permission()
    }

    internal fun answered(granted: Boolean) {
        asking?.complete(granted)
        asking = null
    }

    // --- categories ----------------------------------------------------------

    public fun register(categories: List<NotificationCategory>) {
        synchronized(lock) { write(CATEGORIES, BridgeJson.encodeToString(ListSerializer(NotificationCategory.serializer()), categories)) }
    }

    private fun categories(): List<NotificationCategory> = synchronized(lock) {
        read(CATEGORIES)?.let { runCatching { BridgeJson.decodeFromString(ListSerializer(NotificationCategory.serializer()), it) }.getOrNull() }
            .orEmpty()
    }

    // --- scheduling ----------------------------------------------------------

    /** Shows [request] at its `fireAt`, or at once; replaces a pending one of its id. */
    public fun schedule(request: NotificationRequest, now: Instant = Instant.now()) {
        val at = request.fireAt?.let(::instant)
        synchronized(lock) { save(pending().filter { it.id != request.id }) }
        disarm(request.id)
        when {
            at == null -> show(request)
            !at.isAfter(now) -> Unit
            else -> {
                synchronized(lock) { save(pending() + request) }
                arm(request.id, at)
            }
        }
    }

    public fun cancel(id: String) {
        synchronized(lock) { save(pending().filter { it.id != id }) }
        disarm(id)
        manager.cancel(id, NOTIFICATION_ID)
    }

    public fun cancelThread(thread: String) {
        for (shown in manager.activeNotifications) {
            if (shown.notification.group == thread) manager.cancel(shown.tag, shown.id)
        }
    }

    public fun cancelAll() {
        val ids = synchronized(lock) { pending().map { it.id }.also { save(emptyList()) } }
        ids.forEach(::disarm)
        manager.cancelAll()
    }

    /** The ids scheduled and not yet shown, among those starting with [prefix]. */
    public fun pending(prefix: String): List<String> = synchronized(lock) { pending().map { it.id }.filter { it.startsWith(prefix) } }

    /** What the owner did with the app's notifications, each once. */
    public fun responses(): Flow<NotificationResponse> = answers.receiveAsFlow()

    /**
     * A tap on a notification, or an action that opens the app, as
     * `NotificationOpened` was started with: a response. An intent that
     * reached any other component (the app's exported launcher activity,
     * which anyone may start with any extras) is none. Answers whether it
     * was one.
     */
    internal fun handle(intent: Intent?): Boolean {
        if (intent?.component?.let { it.packageName == app.packageName && it.className == NotificationOpened::class.java.name } != true) return false
        val id = intent.getStringExtra(EXTRA_ID) ?: return false
        respond(intent, id, intent.getStringExtra(EXTRA_ACTION)?.let { NotificationResponse.Action.Named(it) } ?: NotificationResponse.Action.Open)
        intent.removeExtra(EXTRA_ID)
        manager.cancel(id, NOTIFICATION_ID)
        return true
    }

    internal fun respond(intent: Intent, id: String, action: NotificationResponse.Action) {
        val text = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(EXTRA_TEXT)?.toString()
        val info = intent.getBundleExtra(EXTRA_INFO)
        val userInfo = info?.keySet()?.associateWith { info.getString(it).orEmpty() }.orEmpty()
        answers.trySend(NotificationResponse(id = id, category = intent.getStringExtra(EXTRA_CATEGORY), action = action, userInfo = userInfo, text = text))
    }

    /** The alarm for [id] went off: its request is shown, and no longer pending. */
    internal fun fire(id: String) {
        val request = synchronized(lock) {
            val all = pending()
            all.firstOrNull { it.id == id }?.also { found -> save(all.filter { it !== found }) }
        } ?: return
        show(request)
    }

    /** Every pending request's alarm set again: alarms do not outlive a reboot or a force stop. */
    internal fun rearm() {
        for (request in synchronized(lock) { pending() }) {
            request.fireAt?.let(::instant)?.let { arm(request.id, it) }
        }
    }

    // --- showing -------------------------------------------------------------

    private fun show(request: NotificationRequest) {
        if (!request.showsWhileActive && ForegroundActivity.current != null) return
        if (permission() != NotificationPermission.on) return
        ensureChannel()
        val category = request.category?.let { id -> categories().firstOrNull { it.id == id } }
        val builder = Notification.Builder(app, CHANNEL)
            .setSmallIcon(R.drawable.substrate_notification)
            .setContentTitle(request.title)
            .setAutoCancel(true)
            .setCategory(if (request.timeSensitive) Notification.CATEGORY_ALARM else Notification.CATEGORY_REMINDER)
            .setContentIntent(opening(request, null))
        request.body?.let(builder::setContentText)
        request.subtitle?.let(builder::setSubText)
        request.thread?.let(builder::setGroup)
        if (category?.reportsDismissal == true) builder.setDeleteIntent(broadcast(request, ACTION_DISMISS, null, mutable = false))
        for (action in category?.actions.orEmpty()) {
            val input = action.input
            val intent = if (action.opensApp && input == null) opening(request, action.id) else broadcast(request, ACTION_ACTION, action.id, mutable = input != null)
            val button = Notification.Action.Builder(null, action.title, intent)
            if (input != null) button.addRemoteInput(RemoteInput.Builder(EXTRA_TEXT).setLabel(input.placeholder).build())
            builder.addAction(button.build())
        }
        try {
            manager.notify(request.id, NOTIFICATION_ID, builder.build())
        } catch (failure: SecurityException) {
            Log.w(TAG, "a notification was not shown: ${failure.message}")
        }
    }

    private fun ensureChannel() {
        if (manager.getNotificationChannel(CHANNEL) == null) {
            manager.createNotificationChannel(NotificationChannel(CHANNEL, channelName, NotificationManager.IMPORTANCE_HIGH))
        }
    }

    private fun extras(intent: Intent, request: NotificationRequest): Intent = intent
        .putExtra(EXTRA_ID, request.id)
        .putExtra(EXTRA_CATEGORY, request.category)
        .putExtra(EXTRA_INFO, Bundle().apply { request.userInfo.forEach { (key, value) -> putString(key, value) } })

    /** `NotificationOpened`, with what the response needs; it brings the app forward. */
    private fun opening(request: NotificationRequest, action: String?): PendingIntent {
        val intent = extras(Intent(app, NotificationOpened::class.java), request).setData(uri(request.id, action ?: "open"))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_NO_ANIMATION)
        action?.let { intent.putExtra(EXTRA_ACTION, it) }
        return PendingIntent.getActivity(app, request.id.hashCode(), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun broadcast(request: NotificationRequest, what: String, action: String?, mutable: Boolean): PendingIntent {
        val intent = extras(Intent(app, NotificationReceiver::class.java).setAction(what), request).setData(uri(request.id, action ?: what))
        action?.let { intent.putExtra(EXTRA_ACTION, it) }
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or if (mutable) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
        return PendingIntent.getBroadcast(app, request.id.hashCode(), intent, flags)
    }

    // --- alarms --------------------------------------------------------------

    private fun alarm(id: String, create: Boolean): PendingIntent? {
        val intent = Intent(app, NotificationReceiver::class.java).setAction(ACTION_FIRE).setData(uri(id, "fire")).putExtra(EXTRA_ID, id)
        val flags = PendingIntent.FLAG_IMMUTABLE or if (create) PendingIntent.FLAG_UPDATE_CURRENT else PendingIntent.FLAG_NO_CREATE
        return PendingIntent.getBroadcast(app, id.hashCode(), intent, flags)
    }

    private fun arm(id: String, at: Instant) {
        val operation = alarm(id, create = true) ?: return
        val exact = Build.VERSION.SDK_INT < 31 || alarms.canScheduleExactAlarms()
        if (exact) {
            alarms.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at.toEpochMilli(), operation)
        } else {
            alarms.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at.toEpochMilli(), operation)
        }
    }

    private fun disarm(id: String) {
        alarm(id, create = false)?.let { operation ->
            alarms.cancel(operation)
            operation.cancel()
        }
    }

    /** Whether an alarm for [id] is set: what the contract tests hold. */
    internal fun armed(id: String): Boolean = alarm(id, create = false) != null

    // --- the store -----------------------------------------------------------

    private fun pending(): List<NotificationRequest> =
        read(PENDING)?.let { runCatching { BridgeJson.decodeFromString(ListSerializer(NotificationRequest.serializer()), it) }.getOrNull() }
            .orEmpty()

    private fun save(requests: List<NotificationRequest>) {
        write(PENDING, BridgeJson.encodeToString(ListSerializer(NotificationRequest.serializer()), requests))
    }

    private fun read(name: String): String? = File(folder, name).takeIf { it.exists() }?.readText()

    private fun write(name: String, text: String) {
        folder.mkdirs()
        val temporary = File(folder, "$name.tmp")
        temporary.writeText(text)
        if (!temporary.renameTo(File(folder, name))) {
            File(folder, name).writeText(text)
            temporary.delete()
        }
    }

    private fun uri(id: String, what: String): Uri = Uri.Builder().scheme("substrate-notification").authority(what).appendPath(id).build()

    public companion object {
        private const val TAG = "SubstrateNotifications"
        internal const val CHANNEL = "substrate.notifications"
        internal const val NOTIFICATION_ID = 1
        internal const val PENDING = "pending.json"
        internal const val CATEGORIES = "categories.json"
        internal const val ASKED = "asked"
        internal const val ACTION_FIRE = "dev.reamde.substrate.kit.device.NOTIFICATION_FIRE"
        internal const val ACTION_ACTION = "dev.reamde.substrate.kit.device.NOTIFICATION_ACTION"
        internal const val ACTION_DISMISS = "dev.reamde.substrate.kit.device.NOTIFICATION_DISMISS"
        internal const val EXTRA_ID = "dev.reamde.substrate.notification.id"
        internal const val EXTRA_CATEGORY = "dev.reamde.substrate.notification.category"
        internal const val EXTRA_INFO = "dev.reamde.substrate.notification.userInfo"
        internal const val EXTRA_ACTION = "dev.reamde.substrate.notification.action"
        internal const val EXTRA_TEXT = "dev.reamde.substrate.notification.text"

        @Volatile private var shared: Notifications? = null

        /**
         * The process's one instance: the bridge's calls, an alarm and the
         * app's activity reach the same store and the same responses.
         * [channelName] is the system Settings' name for the app's
         * notifications, given once.
         */
        public fun get(context: Context, channelName: String = "Notifications"): Notifications =
            shared ?: synchronized(this) { shared ?: Notifications(context, channelName).also { shared = it } }

        internal fun instant(text: String): Instant? = runCatching { OffsetDateTime.parse(text).toInstant() }.getOrNull()
    }
}
