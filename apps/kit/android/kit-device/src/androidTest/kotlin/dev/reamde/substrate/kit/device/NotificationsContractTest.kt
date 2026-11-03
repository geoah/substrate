package dev.reamde.substrate.kit.device

import android.Manifest
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
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
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * SubstrateDevice's `Notifications` contract on Android: a request with no
 * `fireAt` shows at once, one whose instant passed is dropped, one to come
 * is pending (across instances: the store is a file under no_backup/) with
 * its alarm set until it fires or is cancelled, a request replaces one of
 * its id, `pending(prefix)` lists only that prefix, and a tap is a response,
 * kept until something reads it.
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

    private fun request(id: String, fireAt: Instant?) = NotificationRequest(
        id = prefix + id, title = "Call the plumber", body = "Due at 3:00 PM, in Home", fireAt = fireAt?.toString(), thread = "tasks",
        category = "tasks.reminder", timeSensitive = false, showsWhileActive = true, userInfo = mapOf("task" to id),
    )

    private fun shown(id: String) = manager.activeNotifications.any { it.tag == prefix + id }

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
        assertFalse(shown("passed"))
        assertTrue(notifications.pending(prefix).isEmpty())
        notifications.cancel(prefix + "now")
        assertFalse(shown("now"))
    }

    @Test
    fun aRequestToComeIsPendingWithItsAlarmUntilCancelled() {
        notifications.schedule(request("later", Instant.now().plusSeconds(3600)))
        assertEquals(listOf(prefix + "later"), notifications.pending(prefix))
        assertTrue(notifications.armed(prefix + "later"))
        assertFalse(shown("later"))
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

    @Test
    fun aTapIsAResponseKeptForTheFirstReader() = runBlocking {
        val tap = Intent()
            .putExtra(Notifications.EXTRA_ID, prefix + "tapped")
            .putExtra(Notifications.EXTRA_CATEGORY, "tasks.reminder")
            .putExtra(Notifications.EXTRA_INFO, android.os.Bundle().apply { putString("task", "tapped") })
        assertTrue(notifications.handle(tap))
        assertFalse(notifications.handle(Intent()))
        val response = withTimeout(5_000) { notifications.responses().first() }
        assertEquals(NotificationResponse(id = prefix + "tapped", category = "tasks.reminder", action = NotificationResponse.Action.Open,
                                          userInfo = mapOf("task" to "tapped")), response)
    }
}
