package dev.reamde.substrate.kit.device

import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.reamde.substrate.kit.bridge.SecureItem
import dev.reamde.substrate.kit.bridge.SecureOutcome
import java.io.File
import java.security.KeyStore
import java.util.Base64
import java.util.UUID
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.xmlpull.v1.XmlPullParser

/**
 * The secure store's half of the security parity list (app-kit.md §5.4):
 * missing told from found, ciphertext alone on disk, under no_backup/, which
 * the backup and transfer rules exclude by name, and a corrupt or dropped key
 * answered by a reset, never a crash and never plaintext.
 */
@RunWith(AndroidJUnit4::class)
class SecureStoreContractTest {
    private val context: Context = InstrumentationRegistry.getInstrumentation().targetContext
    private val name = "contract-${UUID.randomUUID()}"
    private val secret = Base64.getEncoder().encodeToString("the session token ✓".toByteArray())
    private val folder = File(context.noBackupFilesDir, "secure-store")

    @Test
    fun missingThenFoundThenMissing() = runBlocking {
        val store = SecureStore(context)
        assertEquals(SecureItem.Missing, store.read(name, "session"))
        assertEquals(SecureOutcome.Done, store.write(name, "session", secret))
        assertEquals(SecureItem.Found(secret), store.read(name, "session"))
        // A process started again reads the same item from disk.
        store.forget(name)
        assertEquals(SecureItem.Found(secret), store.read(name, "session"))
        assertEquals(SecureOutcome.Done, store.delete(name, "session"))
        assertEquals(SecureItem.Missing, store.read(name, "session"))
    }

    @Test
    fun onlyCiphertextIsOnDiskAndItIsUnderNoBackup() = runBlocking {
        SecureStore(context).write(name, "session", secret)
        val files = folder.listFiles().orEmpty().filter { it.name.startsWith(name) }
        assertEquals(setOf("$name.keyset", "$name.store"), files.map { it.name }.toSet())
        assertTrue(folder.canonicalPath.startsWith(context.noBackupFilesDir.canonicalPath))
        for (file in files) {
            val bytes = String(file.readBytes(), Charsets.ISO_8859_1)
            assertFalse(file.name, bytes.contains(secret) || bytes.contains("the session token") || bytes.contains("session"))
        }
    }

    @Test
    fun aCorruptKeysetResetsTheStore() = runBlocking {
        val store = SecureStore(context)
        store.write(name, "session", secret)
        store.forget(name)
        File(folder, "$name.keyset").writeBytes(ByteArray(64) { 7 })
        val first = store.read(name, "session")
        assertTrue("$first", first is SecureItem.Reset)
        assertEquals(SecureItem.Missing, store.read(name, "session"))
    }

    @Test
    fun aCorruptCiphertextResetsTheStore() = runBlocking {
        val store = SecureStore(context)
        store.write(name, "session", secret)
        store.forget(name)
        File(folder, "$name.store").writeBytes(ByteArray(80) { 3 })
        val first = store.read(name, "session")
        assertTrue("$first", first is SecureItem.Reset)
        assertEquals(SecureItem.Missing, store.read(name, "session"))
        // The store works again after a reset.
        assertEquals(SecureOutcome.Done, store.write(name, "session", secret))
        assertEquals(SecureItem.Found(secret), store.read(name, "session"))
    }

    @Test
    fun aKeystoreKeyTheDeviceDroppedResetsTheStore() = runBlocking {
        val store = SecureStore(context)
        store.write(name, "session", secret)
        store.forget(name)
        KeyStore.getInstance("AndroidKeyStore").apply { load(null) }.deleteEntry("dev.reamde.substrate.secure-store.$name")
        val first = store.read(name, "session")
        assertTrue("$first", first is SecureItem.Reset)
        assertEquals(SecureItem.Missing, store.read(name, "session"))
    }

    @Test
    fun theBackupAndTransferRulesExcludeNoBackupByName() {
        fun excludes(resource: String): Map<String, List<String>> {
            val id = context.resources.getIdentifier(resource, "xml", context.packageName)
            assertTrue("no xml/$resource", id != 0)
            val parser = context.resources.getXml(id)
            val found = mutableMapOf<String, MutableList<String>>()
            var section = "full-backup-content"
            while (parser.next() != XmlPullParser.END_DOCUMENT) {
                if (parser.eventType != XmlPullParser.START_TAG) continue
                when (parser.name) {
                    "cloud-backup", "device-transfer" -> section = parser.name
                    "exclude" -> found.getOrPut(section) { mutableListOf() } +=
                        "${parser.getAttributeValue(null, "domain")}:${parser.getAttributeValue(null, "path")}"
                }
            }
            return found
        }
        assertEquals(listOf("root:no_backup/"), excludes("substrate_backup_rules")["full-backup-content"])
        val extraction = excludes("substrate_data_extraction_rules")
        assertEquals(listOf("root:no_backup/"), extraction["cloud-backup"])
        assertEquals(listOf("root:no_backup/"), extraction["device-transfer"])
    }
}
