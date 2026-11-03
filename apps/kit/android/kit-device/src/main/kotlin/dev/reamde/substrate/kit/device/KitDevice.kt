package dev.reamde.substrate.kit.device

import android.content.Context
import dev.reamde.substrate.kit.DeviceAnswer
import dev.reamde.substrate.kit.DeviceServices
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.DNSAnswer
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.NetworkPath
import dev.reamde.substrate.kit.bridge.SecureItem
import dev.reamde.substrate.kit.bridge.SecureOutcome
import dev.reamde.substrate.kit.bridge.TransportChunk
import dev.reamde.substrate.kit.bridge.TransportOutcome
import kotlinx.coroutines.flow.map

/**
 * The device services SubstrateBridge calls, on Android: hand
 * `KitDevice.services(context)` to `Bridge.start`.
 */
public object KitDevice {
    public fun services(context: Context): DeviceServices {
        val app = context.applicationContext
        val transport = Transport(app)
        val secure = SecureStore(app)
        val dns = Dns()
        val connectivity = Connectivity(app)
        return DeviceServices { request ->
            when (request) {
                is DeviceRequest.TransportFetch -> DeviceAnswer.Value {
                    BridgeJson.encodeToJsonElement(TransportOutcome.Serializer, transport.fetch(request.request))
                }
                is DeviceRequest.TransportLines -> DeviceAnswer.Stream(
                    values = transport.lines(request.request).map { BridgeJson.encodeToJsonElement(TransportChunk.Serializer, it) },
                )
                is DeviceRequest.SecureRead -> DeviceAnswer.Value {
                    BridgeJson.encodeToJsonElement(SecureItem.Serializer, secure.read(request.store, request.account))
                }
                is DeviceRequest.SecureWrite -> DeviceAnswer.Value {
                    BridgeJson.encodeToJsonElement(SecureOutcome.Serializer, secure.write(request.store, request.account, request.value))
                }
                is DeviceRequest.SecureDelete -> DeviceAnswer.Value {
                    BridgeJson.encodeToJsonElement(SecureOutcome.Serializer, secure.delete(request.store, request.account))
                }
                is DeviceRequest.DnsTXT -> DeviceAnswer.Value {
                    BridgeJson.encodeToJsonElement(DNSAnswer.Serializer, dns.txt(request.name, request.timeoutMillis))
                }
                DeviceRequest.ConnectivityCurrent -> DeviceAnswer.Value {
                    BridgeJson.encodeToJsonElement(NetworkPath.serializer(), connectivity.current())
                }
                DeviceRequest.ConnectivityWatch -> DeviceAnswer.Stream(
                    values = connectivity.paths().map { BridgeJson.encodeToJsonElement(NetworkPath.serializer(), it) },
                )
            }
        }
    }
}
