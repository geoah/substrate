package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.BridgeQuery
import dev.reamde.substrate.kit.bridge.BridgeStats
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.EnvironmentUpdate
import dev.reamde.substrate.kit.bridge.Handle
import dev.reamde.substrate.kit.bridge.ListMessage
import dev.reamde.substrate.kit.bridge.Op
import dev.reamde.substrate.kit.bridge.OpenRequest
import dev.reamde.substrate.kit.bridge.ResyncRequest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonElement

/**
 * The bridge's Kotlin half, apart from JNI: what Swift's messages do to the
 * handles' state and which device calls they start, and what Kotlin posts.
 * `post` is Swift's `post`; on a device it is the native, in a test a fake.
 */
public class BridgeCore(
    private val post: (String) -> String?,
    private val services: DeviceServices,
    private val scope: CoroutineScope,
    private val log: ((String) -> Unit)? = null,
) {
    private class Open(val handle: Handle, val flow: MutableStateFlow<HandleSnapshot>) {
        val lists = HashMap<String, KeyedListState>()
    }

    private val handles = ConcurrentHashMap<Long, Open>()

    /**
     * Held from posting `open` until its handle is registered here. Swift may
     * deliver the handle's first presentation before `post` returns, from
     * its own thread; a message for a handle not yet known waits on this, so
     * an idle screen's only presentation is never dropped.
     */
    private val opening = Any()
    private val calls = ConcurrentHashMap<Long, Job>()
    private val queries = ConcurrentHashMap<Long, CompletableDeferred<JsonElement>>()
    private val queryIDs = AtomicLong(0)
    private val failures = AtomicLong(0)

    /** Messages from Swift this side could not read or act on. */
    public val failureCount: Long get() = failures.get()

    // MARK: Kotlin to Swift

    public fun open(root: String, settings: JsonElement? = null): Result<BridgeHandle> = synchronized(opening) {
        val reply = send(Envelope(v = 1, op = Op.open, body = BridgeJson.encodeToJsonElement(OpenRequest.serializer(), OpenRequest(root, settings))))
            .getOrElse { return Result.failure(it) }
        val handle = reply.handle ?: return Result.failure(BridgeException(BridgeFailure(BridgeFailure.Code.malformed, "opened names no handle")))
        val open = Open(handle, MutableStateFlow(HandleSnapshot()))
        handles[handle.id] = open
        Result.success(BridgeHandle(handle, open.flow, this))
    }

    /** The device's locale or time zone changed: every open root hears it. */
    public fun environment(update: EnvironmentUpdate): Result<Unit> =
        send(Envelope(v = 1, op = Op.environment, body = BridgeJson.encodeToJsonElement(EnvironmentUpdate.serializer(), update))).map { }

    internal fun command(handle: Handle, command: JsonElement): Result<Unit> =
        send(Envelope(v = 1, op = Op.command, handle = handle, body = command)).map { }

    internal fun close(handle: Handle): Result<Unit> =
        send(Envelope(v = 1, op = Op.close, handle = handle)).map { }

    /** The bridge's counters, from Swift. */
    public suspend fun stats(): BridgeStats {
        val id = queryIDs.incrementAndGet()
        val answer = CompletableDeferred<JsonElement>()
        queries[id] = answer
        send(Envelope(v = 1, op = Op.request, id = id, body = BridgeJson.encodeToJsonElement(BridgeQuery.Serializer, BridgeQuery.Stats)))
            .onFailure { queries.remove(id); throw it }
        return BridgeJson.decodeFromJsonElement(BridgeStats.serializer(), answer.await())
    }

    /** Posts one envelope; Swift's answer, or the failure it answered with. */
    private fun send(envelope: Envelope, call: Redaction.Call = Redaction.Call.UNKNOWN): Result<Envelope> {
        val text = BridgeJson.encodeToString(Envelope.serializer(), envelope)
        log?.invoke("in ${Redaction.redact(text, call)}")
        val reply = post(text) ?: return Result.failure(BridgeException(BridgeFailure(BridgeFailure.Code.jniFailure, "post answered nothing")))
        val answer = try {
            BridgeJson.decodeFromString(Envelope.serializer(), reply)
        } catch (_: Exception) {
            return Result.failure(BridgeException(BridgeFailure(BridgeFailure.Code.malformed, "post answered something that is not an envelope")))
        }
        if (answer.op == Op.error) return Result.failure(BridgeException(failure(answer.body)))
        return Result.success(answer)
    }

    // MARK: Swift to Kotlin

    /** One message from Swift. Never throws: what it cannot act on is counted. */
    public fun receive(message: String) {
        val envelope = try {
            BridgeJson.decodeFromString(Envelope.serializer(), message)
        } catch (_: Exception) {
            failures.incrementAndGet()
            log?.invoke("unreadable ${Redaction.redact(message)}")
            return
        }
        // Swift answers only Kotlin's queries, which carry no secret.
        val call = if (envelope.id?.let { queries.containsKey(it) } == true) Redaction.Call.PUBLIC else Redaction.Call.UNKNOWN
        log?.invoke("out ${Redaction.redact(message, call)}")
        when (envelope.op) {
            Op.presentation -> update(envelope.handle) { it.copy(presentation = envelope.body).rebuilt() }
            Op.list -> list(envelope)
            Op.closed -> {
                val open = opened(envelope.handle)
                if (open != null && handles.remove(open.handle.id, open)) {
                    synchronized(open) { open.flow.value = open.flow.value.copy(closed = true) }
                } else {
                    failures.incrementAndGet()
                }
            }
            Op.request -> request(envelope)
            Op.cancel -> envelope.id?.let { calls.remove(it)?.cancel() }
            Op.reply -> envelope.id?.let { id -> queries.remove(id)?.complete(envelope.body ?: kotlinx.serialization.json.JsonNull) }
            Op.error -> {
                val failure = failure(envelope.body)
                val query = envelope.id?.let { queries.remove(it) }
                when {
                    query != null -> query.completeExceptionally(BridgeException(failure))
                    envelope.handle != null -> update(envelope.handle) { it.copy(problem = failure) }
                    else -> failures.incrementAndGet()
                }
            }
            else -> failures.incrementAndGet()
        }
    }

    /** The handle's state, unless it is closed or of another generation. */
    private fun opened(handle: Handle?): Open? {
        handle ?: return null
        fun find() = handles[handle.id]?.takeIf { it.handle == handle }
        // Not known yet: an open may be between Swift's answer and this side's registration.
        return find() ?: synchronized(opening) { find() }
    }

    private fun update(handle: Handle?, change: (HandleSnapshot) -> HandleSnapshot) {
        val open = opened(handle) ?: run {
            failures.incrementAndGet()
            return
        }
        synchronized(open) { open.flow.value = change(open.flow.value) }
    }

    private fun list(envelope: Envelope) {
        val handle = envelope.handle
        val open = opened(handle) ?: run {
            failures.incrementAndGet()
            return
        }
        val message = try {
            BridgeJson.decodeFromJsonElement(ListMessage.Serializer, envelope.body ?: return)
        } catch (_: Exception) {
            failures.incrementAndGet()
            return
        }
        val name = when (message) {
            is ListMessage.Whole -> message.list
            is ListMessage.Delta -> message.list
        }
        // Applied and published under the handle's lock, so the snapshot a
        // collector sees has every delta before it applied, in order.
        val resync = synchronized(open) {
            val state = open.lists.getOrPut(name) { KeyedListState(name) }
            val applied = state.apply(message)
            if (applied == KeyedListState.Applied.APPLIED) {
                val snapshot = open.flow.value
                open.flow.value = snapshot.copy(lists = snapshot.lists + (name to state.snapshot),
                    revisions = snapshot.revisions + (name to state.revision)).rebuilt()
            }
            applied == KeyedListState.Applied.NEEDS_WHOLE
        }
        if (resync) {
            send(Envelope(v = 1, op = Op.resync, handle = handle, body = BridgeJson.encodeToJsonElement(ResyncRequest.serializer(), ResyncRequest(name))))
        }
    }

    private fun request(envelope: Envelope) {
        val id = envelope.id ?: return run { failures.incrementAndGet() }
        val request = try {
            BridgeJson.decodeFromJsonElement(DeviceRequest.Serializer, envelope.body ?: return)
        } catch (_: Exception) {
            answerFailure(envelope, BridgeFailure(BridgeFailure.Code.malformed, "a request Kotlin cannot read"))
            return
        }
        val sensitive = Redaction.carriesSecrets(request)
        val call = if (sensitive) Redaction.Call.SECRET else Redaction.Call.PUBLIC
        val answer = try {
            services.answer(request)
        } catch (failure: Exception) {
            DeviceAnswer.Unsupported("the device services threw ${failure::class.java.simpleName}")
        }
        val answered = AtomicBoolean(false)

        /** True when this was the answer and Swift took it. */
        fun once(message: Envelope): Boolean = answered.compareAndSet(false, true) && send(message, call).isSuccess
        when (answer) {
            is DeviceAnswer.Unsupported ->
                once(errorEnvelope(envelope, BridgeFailure(BridgeFailure.Code.unsupported, answer.reason)))
            is DeviceAnswer.Value -> launch(id) {
                // Kept as soon as it is made: a cancel that lands after the
                // block returned still leaves it this side's to discard.
                val made = AtomicReference<JsonElement?>(null)
                val body = try {
                    withContext(answer.dispatcher) { answer.block().also { made.set(it) } }
                } catch (cancelled: CancellationException) {
                    made.get()?.let(answer.discard)
                    throw cancelled
                } catch (failure: Exception) {
                    once(errorEnvelope(envelope, failureOf(failure, sensitive)))
                    return@launch
                }
                if (!once(Envelope(v = 1, op = Op.reply, handle = envelope.handle, id = id, body = body))) answer.discard(body)
            }
            is DeviceAnswer.Chunks -> launch(id) {
                var seq = 0L
                try {
                    withContext(answer.dispatcher) {
                        answer.values.collect { value ->
                            val posted = send(Envelope(v = 1, op = Op.chunk, handle = envelope.handle, id = id, seq = seq++, body = value), call)
                            // Swift refused it: its queue overflowed, or its reader went away.
                            if (posted.isFailure) throw CancellationException("Swift refused the chunk")
                        }
                    }
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (failure: Exception) {
                    once(errorEnvelope(envelope, failureOf(failure, sensitive)))
                    return@launch
                }
                once(Envelope(v = 1, op = Op.end, handle = envelope.handle, id = id))
            }
        }
    }

    private fun launch(id: Long, block: suspend CoroutineScope.() -> Unit) {
        val job = scope.launch(start = CoroutineStart.LAZY, block = block)
        calls[id] = job
        job.invokeOnCompletion { calls.remove(id, job) }
        job.start()
    }

    private fun errorEnvelope(request: Envelope, failure: BridgeFailure) =
        Envelope(v = 1, op = Op.error, handle = request.handle, id = request.id, body = BridgeJson.encodeToJsonElement(BridgeFailure.serializer(), failure))

    private fun answerFailure(request: Envelope, failure: BridgeFailure) {
        send(errorEnvelope(request, failure))
    }

    /**
     * A service's throw: the failure a `BridgeException` names (a chunked answer's
     * `overflow`), else by type alone where its message might hold a secret.
     */
    private fun failureOf(failure: Exception, sensitive: Boolean) = (failure as? BridgeException)?.failure ?: BridgeFailure(
        BridgeFailure.Code.kotlinException,
        if (sensitive) failure::class.java.simpleName else "${failure::class.java.simpleName}: ${failure.message}",
    )

    private fun failure(body: JsonElement?): BridgeFailure = body?.let {
        runCatching { BridgeJson.decodeFromJsonElement(BridgeFailure.serializer(), it) }.getOrNull()
    } ?: BridgeFailure(BridgeFailure.Code.malformed, "an error Kotlin cannot read")

    internal fun recordFailure(what: String) {
        failures.incrementAndGet()
        log?.invoke(what)
    }

    /** Calls Swift asked for that are still running. */
    public val runningCalls: Int get() = calls.size
}

/** A failure the bridge answered with, as a Kotlin exception. */
public class BridgeException(public val failure: BridgeFailure) : Exception("${failure.code}: ${failure.message}")
