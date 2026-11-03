package dev.reamde.substrate.kit.device

import android.content.Context
import android.security.keystore.KeyPermanentlyInvalidatedException
import androidx.datastore.core.CorruptionException
import androidx.datastore.core.DataStore
import androidx.datastore.core.DataStoreFactory
import androidx.datastore.core.Serializer
import com.google.crypto.tink.Aead
import com.google.crypto.tink.KeysetHandle
import com.google.crypto.tink.RegistryConfiguration
import com.google.crypto.tink.TinkProtoKeysetFormat
import com.google.crypto.tink.aead.AeadConfig
import com.google.crypto.tink.aead.PredefinedAeadParameters
import com.google.crypto.tink.integration.android.AndroidKeystore
import dev.reamde.substrate.kit.bridge.SecureItem
import dev.reamde.substrate.kit.bridge.SecureOutcome
import java.io.File
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.security.GeneralSecurityException
import java.util.concurrent.ConcurrentHashMap
import javax.crypto.AEADBadTagException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json

/**
 * The secure store (docs/plans/app-kit.md §5.4): per store name, a Tink
 * AES-256-GCM keyset wrapped by an Android Keystore key, and one DataStore
 * file holding the store's items as one ciphertext, both under
 * `noBackupFilesDir`. The Keystore key never leaves the device, and the
 * folder is in credential-encrypted storage, so an item is readable after
 * the first unlock and on this device only.
 *
 * A keyset or a ciphertext that will not decrypt, or a Keystore key the
 * device dropped, is not a crash and not a plaintext fallback: the store's
 * files and key are deleted and the read answers `reset`, which the core
 * reads as missing, so the app signs in again. A Keystore or a disk that
 * cannot answer now is `unavailable`, and nothing is deleted.
 */
public class SecureStore(context: Context) {
    private val folder = File(context.noBackupFilesDir, "secure-store")

    init {
        AeadConfig.register()
    }

    public suspend fun read(store: String, account: String): SecureItem = guarded(store) { opened ->
        when (val value = opened.items()[account]) {
            null -> SecureItem.Missing
            else -> SecureItem.Found(value)
        }
    }.fold({ it }, { problem -> problem.item() })

    public suspend fun write(store: String, account: String, value: String): SecureOutcome = guarded(store) { opened ->
        opened.update { it + (account to value) }
        SecureOutcome.Done
    }.fold({ it }, { problem -> SecureOutcome.Unavailable(problem.reason) })

    public suspend fun delete(store: String, account: String): SecureOutcome = guarded(store) { opened ->
        opened.update { it - account }
        SecureOutcome.Done
    }.fold({ it }, { problem -> SecureOutcome.Unavailable(problem.reason) })

    private sealed interface Problem {
        val reason: String

        fun item(): SecureItem = when (this) {
            is Reset -> SecureItem.Reset(reason)
            is Unavailable -> SecureItem.Unavailable(reason)
        }
    }

    private class Reset(override val reason: String) : Problem

    private class Unavailable(override val reason: String) : Problem

    /** Lets go of a store as a process exit would, so a test reads it again from disk. */
    internal fun forget(name: String) {
        stores.remove(name)?.release()
    }

    private suspend fun <T> guarded(name: String, body: suspend (Store) -> T): Result<T, Problem> {
        val store = stores.getOrPut(name) { Store(name, folder) }
        return try {
            Result.Ok(body(store))
        } catch (failure: Exception) {
            val problem = classify(failure)
            if (problem is Reset) {
                stores.remove(name)
                store.destroy()
            }
            Result.Err(problem)
        }
    }

    /** A store that cannot be decrypted is reset; a Keystore or a disk that failed is not. */
    private fun classify(failure: Throwable): Problem {
        val chain = generateSequence(failure) { it.cause }.toList()
        return when {
            chain.any { it is KeyPermanentlyInvalidatedException } -> Reset("the device invalidated the store's key")
            chain.any { it is MissingKey } -> Reset("the device no longer holds the store's key")
            chain.any { it is AEADBadTagException || it is CorruptionException || it is CorruptKeyset } ->
                Reset("the store would not decrypt")
            chain.any { it is GeneralSecurityException } -> Unavailable("the Keystore could not answer (${failure::class.java.simpleName})")
            chain.any { it is IOException } -> Unavailable("the store could not be read (${failure::class.java.simpleName})")
            else -> Unavailable("the store failed (${failure::class.java.simpleName})")
        }
    }

    private sealed interface Result<out T, out E> {
        class Ok<T>(val value: T) : Result<T, Nothing>

        class Err<E>(val error: E) : Result<Nothing, E>
    }

    private inline fun <T, E, R> Result<T, E>.fold(ok: (T) -> R, err: (E) -> R): R = when (this) {
        is Result.Ok -> ok(value)
        is Result.Err -> err(error)
    }

    private class MissingKey : GeneralSecurityException("the Keystore key is gone")

    private class CorruptKeyset(cause: Throwable) : GeneralSecurityException("the store would not decrypt", cause)

    /** One store: its keyset, its Keystore key and its DataStore file. */
    private class Store(private val name: String, folder: File) {
        private val alias = "dev.reamde.substrate.secure-store.$name"
        private val keysetFile = File(folder, "$name.keyset")
        private val dataFile = File(folder, "$name.store")
        private val associated = "substrate secure store $name".toByteArray()
        private val json = Json

        @Volatile private var aead: Aead? = null

        // Its own scope, so a reset releases the file: DataStore refuses a
        // second instance over a file while the first is active.
        private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())

        private val data: DataStore<ByteArray> by lazy {
            folder.mkdirs()
            DataStoreFactory.create(serializer = Ciphertext, scope = scope, produceFile = { dataFile })
        }

        suspend fun items(): Map<String, String> = decode(data.data.first())

        suspend fun update(change: (Map<String, String>) -> Map<String, String>) {
            data.updateData { current -> encode(change(decode(current))) }
        }

        private fun decode(ciphertext: ByteArray): Map<String, String> {
            if (ciphertext.isEmpty()) return emptyMap()
            val aead = aead()
            // The keyset's own AES-GCM, in this process: a failure here is the
            // file, never the Keystore.
            val plaintext = try {
                aead.decrypt(ciphertext, associated)
            } catch (failure: GeneralSecurityException) {
                throw CorruptKeyset(failure)
            }
            return try {
                json.decodeFromString(ITEMS, plaintext.decodeToString())
            } catch (failure: Exception) {
                throw CorruptKeyset(failure)
            }
        }

        private fun encode(items: Map<String, String>): ByteArray =
            if (items.isEmpty()) ByteArray(0) else aead().encrypt(json.encodeToString(ITEMS, items).toByteArray(), associated)

        @Synchronized
        private fun aead(): Aead {
            aead?.let { return it }
            val handle = if (keysetFile.exists()) {
                if (!AndroidKeystore.hasKey(alias)) throw MissingKey()
                val wrapped = keysetFile.readBytes()
                try {
                    TinkProtoKeysetFormat.parseEncryptedKeyset(wrapped, AndroidKeystore.getAead(alias), associated, RegistryConfiguration.get())
                } catch (failure: GeneralSecurityException) {
                    throw if (failure.isCorruption()) CorruptKeyset(failure) else failure
                }
            } else {
                if (!AndroidKeystore.hasKey(alias)) AndroidKeystore.generateNewAes256GcmKey(alias)
                val generated = KeysetHandle.generateNew(PredefinedAeadParameters.AES256_GCM, RegistryConfiguration.get())
                keysetFile.parentFile?.mkdirs()
                val tmp = File(keysetFile.path + ".tmp")
                tmp.writeBytes(TinkProtoKeysetFormat.serializeEncryptedKeyset(generated, AndroidKeystore.getAead(alias), associated, RegistryConfiguration.get()))
                if (!tmp.renameTo(keysetFile)) throw IOException("the keyset could not be written")
                generated
            }
            return handle.getPrimitive(RegistryConfiguration.get(), Aead::class.java).also { aead = it }
        }

        fun release() {
            scope.cancel()
        }

        /** Deletes the store's files and key: what reset means. */
        fun destroy() {
            release()
            keysetFile.delete()
            dataFile.delete()
            runCatching { AndroidKeystore.deleteKey(alias) }
        }

        private fun GeneralSecurityException.isCorruption(): Boolean =
            generateSequence<Throwable>(this) { it.cause }.any { it is AEADBadTagException } ||
                message.orEmpty().contains("decryption failed", ignoreCase = true) ||
                message.orEmpty().contains("parse keyset failed", ignoreCase = true) ||
                message.orEmpty().contains("invalid keyset", ignoreCase = true)

        companion object {
            private val ITEMS = MapSerializer(String.serializer(), String.serializer())
        }
    }

    private companion object {
        // One per process, whatever SecureStore is made: DataStore refuses a
        // second instance over one file.
        val stores = ConcurrentHashMap<String, Store>()
    }

    /** DataStore's file as raw bytes: the ciphertext, and nothing else. */
    private object Ciphertext : Serializer<ByteArray> {
        override val defaultValue: ByteArray = ByteArray(0)

        override suspend fun readFrom(input: InputStream): ByteArray = input.readBytes()

        override suspend fun writeTo(t: ByteArray, output: OutputStream) {
            output.write(t)
        }
    }
}
