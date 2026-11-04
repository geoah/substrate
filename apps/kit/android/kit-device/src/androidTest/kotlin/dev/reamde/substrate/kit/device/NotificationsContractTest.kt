package dev.reamde.substrate.kit.device

import android.Manifest
import android.app.ActivityOptions
import android.app.NotificationManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.reamde.substrate.kit.bridge.NotificationPermission
import dev.reamde.substrate.kit.bridge.NotificationRequest
import dev.reamde.substrate.kit.bridge.NotificationResponse
import java.io.File
import java.time.Instant
import java.util.UUID
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * SubstrateDevice's `Notifications` contract on Android: a request with no `fireAt` shows at once, one whose instant passed is dropped, one to come is pending
 * (across instances: the store is a file under no_backup/) with its alarm set until it fires or is cancelled, a request replaces one of its id,
 * `pending(prefix)` lists only that prefix, and a tap is a response, kept until something reads it, made only from what reached the app's own unexported
 * `NotificationOpened`, never from an intent another app built.
 */
@RunWith(AndroidJUnit4::class)
class NotificationsContractTest {
    private val context: Context = InstrumentationRegistry.getInstrumentation().targetContext
    private val manager = context.getSystemService(NotificationManager::class.java)
    private val prefix = "contract.${UUID.randomUUID()}."
    private lateinit var notifications: Notifications

    @Before
    fun setUp() {
        InstrumentationRegistry.getInstrumentation().uiAutomation.grantRuntimePermission(context.packageName, Manifest.permission.POST_NOTIFICATIONS)
        notifications = Notifications(context, "Contract")
    }

    @After
    fun tearDown() {
        notifications.cancelAll()
    }

    private fun request(id: String, fireAt: Instant?) =
        NotificationRequest(
            id = prefix + id,
            title = "Call the plumber",
            body = "Due at 3:00 PM, in Home",
            fireAt = fireAt?.toString(),
            thread = "tasks",
            category = "tasks.reminder",
            timeSensitive = false,
            showsWhileActive = true,
            userInfo = mapOf("task" to id),
        )

    private fun isShown(id: String) = manager.activeNotifications.any { it.tag == prefix + id }

    /**
     * Whether [id] is shown, given the moment `notify` and `cancel` take to reach the system: seconds on an emulator still starting the activity a tap opened.
     */
    private fun shown(id: String, expected: Boolean = true): Boolean {
        val until = System.currentTimeMillis() + 15_000
        while (isShown(id) != expected && System.currentTimeMillis() < until) Thread.sleep(50)
        return isShown(id)
    }

    @Test
    fun permissionIsOnOnceGranted() {
        assertEquals(NotificationPermission.on, notifications.permission())
    }

    @Test
    fun noInstantShowsAtOnceAndAPassedOneIsDropped() {
        notifications.schedule(request("now", null))
        assertTrue(shown("now"))
        assertFalse(prefix + "now" in notifications.pending(prefix))
        notifications.schedule(request("passed", Instant.now().minusSeconds(60)))
        assertFalse(shown("passed", expected = false))
        assertTrue(notifications.pending(prefix).isEmpty())
        notifications.cancel(prefix + "now")
        assertFalse(shown("now", expected = false))
    }

    @Test
    fun aRequestToComeIsPendingWithItsAlarmUntilCancelled() {
        notifications.schedule(request("later", Instant.now().plusSeconds(3600)))
        assertEquals(listOf(prefix + "later"), notifications.pending(prefix))
        assertTrue(notifications.armed(prefix + "later"))
        assertFalse(shown("later", expected = false))
        // A process started again reads the same file.
        assertEquals(listOf(prefix + "later"), Notifications(context, "Contract").pending(prefix))
        notifications.cancel(prefix + "later")
        assertTrue(notifications.pending(prefix).isEmpty())
        assertFalse(notifications.armed(prefix + "later"))
    }

    @Test
    fun aRequestReplacesOneOfItsIdAndPendingListsOnlyItsPrefix() {
        notifications.schedule(request("a", Instant.now().plusSeconds(3600)))
        notifications.schedule(request("a", Instant.now().plusSeconds(7200)))
        notifications.schedule(request("b", Instant.now().plusSeconds(3600)))
        notifications.schedule(request("a", Instant.now().plusSeconds(3600)).copy(id = "other.${UUID.randomUUID()}"))
        assertEquals(listOf(prefix + "a", prefix + "b"), notifications.pending(prefix).sorted())
        notifications.cancelAll()
        assertTrue(notifications.pending(prefix).isEmpty())
        assertTrue(notifications.pending("other.").isEmpty())
    }

    @Test
    fun anAlarmThatFiresShowsItsRequestOnce() {
        notifications.schedule(request("due", Instant.now().plusSeconds(3600)))
        notifications.fire(prefix + "due")
        assertTrue(shown("due"))
        assertTrue(notifications.pending(prefix).isEmpty())
        notifications.fire(prefix + "due")
        notifications.cancel(prefix + "due")
    }

    @Test
    fun theStoreIsUnderNoBackup() {
        notifications.schedule(request("kept", Instant.now().plusSeconds(3600)))
        val file = File(context.noBackupFilesDir, "notifications/pending.json")
        assertTrue(file.exists())
        assertTrue(file.readText().contains(prefix + "kept"))
    }

    private fun tap(component: ComponentName, id: String) =
        Intent()
            .setComponent(component)
            .putExtra(Notifications.EXTRA_ID, prefix + id)
            .putExtra(Notifications.EXTRA_CATEGORY, "tasks.reminder")
            .putExtra(Notifications.EXTRA_INFO, android.os.Bundle().apply { putString("task", id) })

    @Test
    fun aTapIsAResponseKeptForTheFirstReader() = runBlocking {
        assertTrue(notifications.handle(tap(ComponentName(context, NotificationOpened::class.java), "tapped")))
        assertFalse(notifications.handle(Intent(context, NotificationOpened::class.java)))
        val response = withTimeout(5_000) { notifications.responses().first() }
        assertEquals(
            NotificationResponse(
                id = prefix + "tapped",
                category = "tasks.reminder",
                action = NotificationResponse.Action.Open,
                userInfo = mapOf("task" to "tapped"),
            ),
            response,
        )
    }

    /**
     * An app's launcher activity is exported, so any app may start it with any extras: what reaches it, or any component but the one the notifications' own
     * intents name, is no response.
     */
    @Test
    fun aTapForgedThroughTheLauncherIsNoResponse() = runBlocking {
        val launcher = ComponentName(context.packageName, "${context.packageName}.MainActivity")
        assertFalse(notifications.handle(tap(launcher, "forged")))
        assertFalse(notifications.handle(tap(ComponentName("com.example.other", NotificationOpened::class.java.name), "forged")))
        assertFalse(notifications.handle(tap(ComponentName(context, NotificationReceiver::class.java), "forged")))
        assertNull(withTimeoutOrNull(500) { notifications.responses().first() })
    }

    @Test
    fun theActivityATapLandsOnIsNotExported() {
        val info = context.packageManager.getActivityInfo(ComponentName(context, NotificationOpened::class.java), 0)
        assertFalse(info.exported)
    }

    /** A shown notification's own tap, its content intent sent, reaches the process's instance as a response. */
    @Test
    fun aShownNotificationsTapIsAResponse() = runBlocking {
        val shared = Notifications.get(context)
        shared.schedule(request("shown", null))
        assertTrue(shown("shown"))
        val notification = manager.activeNotifications.first { it.tag == prefix + "shown" }.notification
        val options =
            if (Build.VERSION.SDK_INT >= 34) {
                ActivityOptions.makeBasic().setPendingIntentBackgroundActivityStartMode(ActivityOptions.MODE_BACKGROUND_ACTIVITY_START_ALLOWED).toBundle()
            } else {
                null
            }
        notification.contentIntent.send(context, 0, null, null, null, null, options)
        val response = withTimeout(30_000) { shared.responses().first() }
        assertEquals(
            NotificationResponse(
                id = prefix + "shown",
                category = "tasks.reminder",
                action = NotificationResponse.Action.Open,
                userInfo = mapOf("task" to "shown"),
            ),
            response,
        )
        shared.cancel(prefix + "shown")
    }
}
