package dev.reamde.substrate.kit.device

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import dev.reamde.substrate.kit.BridgeException
import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.NetworkPath
import kotlinx.coroutines.channels.ProducerScope
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.distinctUntilChanged

/**
 * The default network: online when it reaches the internet (validated),
 * expensive when it is metered, constrained when the owner turned data saver
 * on for this app.
 */
public class Connectivity(context: Context) {
    private val manager = context.getSystemService(ConnectivityManager::class.java)

    public fun current(): NetworkPath = path(manager.activeNetwork?.let { manager.getNetworkCapabilities(it) })

    /**
     * The current path, then each change, until the collector stops. A
     * change is never dropped: one the queue of [capacity] has no room for
     * ends the flow with an `overflow`, which Swift hears as the chunked
     * answer's error, and the callback is unregistered with it.
     */
    public fun paths(capacity: Int = 64): Flow<NetworkPath> = callbackFlow {
        fun offer(path: NetworkPath) = sendOrOverflow(path, "network changes came faster than they were read")
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
                offer(path(capabilities))
            }

            override fun onLost(network: Network) {
                offer(current())
            }
        }
        offer(current())
        manager.registerDefaultNetworkCallback(callback)
        awaitClose { manager.unregisterNetworkCallback(callback) }
    }.buffer(capacity).distinctUntilChanged()

    private fun path(capabilities: NetworkCapabilities?): NetworkPath {
        val online = capabilities != null &&
            capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
        val expensive = capabilities != null && !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)
        val constrained = manager.restrictBackgroundStatus == ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED
        return NetworkPath(online = online, expensive = online && expensive, constrained = online && constrained)
    }
}

/**
 * Sends [value] from a callback, or, when the flow's queue is full, ends
 * the flow with an `overflow` that says [what]: a device answer is never
 * dropped silently. A send after the flow ended is nobody's to read.
 */
internal fun <T> ProducerScope<T>.sendOrOverflow(value: T, what: String) {
    val sent = trySend(value)
    if (sent.isFailure && !sent.isClosed) close(BridgeException(BridgeFailure(BridgeFailure.Code.overflow, what)))
}
