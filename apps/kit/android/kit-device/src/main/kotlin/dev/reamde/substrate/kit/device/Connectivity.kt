package dev.reamde.substrate.kit.device

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import dev.reamde.substrate.kit.bridge.NetworkPath
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
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

    /** The current path, then each change, until the collector stops. */
    public fun paths(): Flow<NetworkPath> = callbackFlow {
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
                trySend(path(capabilities))
            }

            override fun onLost(network: Network) {
                trySend(current())
            }
        }
        trySend(current())
        manager.registerDefaultNetworkCallback(callback)
        awaitClose { manager.unregisterNetworkCallback(callback) }
    }.distinctUntilChanged()

    private fun path(capabilities: NetworkCapabilities?): NetworkPath {
        val online = capabilities != null &&
            capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
        val expensive = capabilities != null && !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)
        val constrained = manager.restrictBackgroundStatus == ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED
        return NetworkPath(online = online, expensive = online && expensive, constrained = online && constrained)
    }
}
