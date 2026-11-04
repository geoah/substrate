package dev.reamde.substrate.kit.device

import android.content.Context
import dev.reamde.substrate.kit.DeviceAnswer
import dev.reamde.substrate.kit.DeviceServices
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.BrowserAnswer
import dev.reamde.substrate.kit.bridge.BrowserPrepared
import dev.reamde.substrate.kit.bridge.DNSAnswer
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.NetworkPath
import dev.reamde.substrate.kit.bridge.NotificationPermission
import dev.reamde.substrate.kit.bridge.NotificationResponse
import dev.reamde.substrate.kit.bridge.SecureItem
import dev.reamde.substrate.kit.bridge.SecureOutcome
import dev.reamde.substrate.kit.bridge.TransportChunk
import dev.reamde.substrate.kit.bridge.TransportOutcome
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.map
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.JsonNull

/**
 * The device services SubstrateBridge calls, on Android: hand `KitDevice.services(context)` to `Bridge.start`. [notificationChannel] is the name the system's
 * Settings give the app's notifications.
 */
public object KitDevice {
    public fun services(context: Context, notificationChannel: String = "Notifications"): DeviceServices =
        services(context, notificationChannel, BrowserSignIn(context.applicationContext))

    /** The same over [browser]: a contract test's, which opens no browser. */
    internal fun services(context: Context, notificationChannel: String, browser: BrowserSignIn): DeviceServices {
        val app = context.applicationContext
        val transport = Transport(app)
        val secure = SecureStore(app)
        val dns = Dns()
        val connectivity = Connectivity(app)
        val notifications = Notifications.get(app, notificationChannel)
        val done = { JsonNull }
        return DeviceServices { request ->
            when (request) {
                is DeviceRequest.TransportFetch ->
                    DeviceAnswer.Value(
                        discard = { answer ->
                            runCatching { BridgeJson.decodeFromJsonElement(TransportOutcome.Serializer, answer) }.getOrNull()?.let(transport::discard)
                        }
                    ) {
                        BridgeJson.encodeToJsonElement(TransportOutcome.Serializer, transport.fetch(request.request))
                    }
                is DeviceRequest.TransportLines ->
                    DeviceAnswer.Chunks(values = transport.lines(request.request).map { BridgeJson.encodeToJsonElement(TransportChunk.Serializer, it) })
                is DeviceRequest.SecureRead ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(SecureItem.Serializer, secure.read(request.store, request.account))
                    }
                is DeviceRequest.SecureWrite ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(SecureOutcome.Serializer, secure.write(request.store, request.account, request.value))
                    }
                is DeviceRequest.SecureDelete ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(SecureOutcome.Serializer, secure.delete(request.store, request.account))
                    }
                is DeviceRequest.DnsTXT ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(DNSAnswer.Serializer, dns.txt(request.name, request.timeoutMillis))
                    }
                DeviceRequest.ConnectivityCurrent ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(NetworkPath.serializer(), connectivity.current())
                    }
                DeviceRequest.ConnectivityWatch ->
                    DeviceAnswer.Chunks(values = connectivity.paths().map { BridgeJson.encodeToJsonElement(NetworkPath.serializer(), it) })
                // A route Swift never took (its call cancelled, its handle
                // closed) is let go of here: no `finish` will name it.
                is DeviceRequest.BrowserPrepare ->
                    DeviceAnswer.Value(
                        discard = { answer ->
                            val prepared = runCatching { BridgeJson.decodeFromJsonElement(BrowserPrepared.Serializer, answer) }.getOrNull()
                            if (prepared is BrowserPrepared.Ready) browser.discard(prepared.redirect)
                        }
                    ) {
                        BridgeJson.encodeToJsonElement(BrowserPrepared.Serializer, browser.prepare(request.route))
                    }
                is DeviceRequest.BrowserOpen ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(BrowserAnswer.Serializer, browser.open(request.page, request.redirect))
                    }
                // Acted on as it arrives, in the order Swift sent it, never
                // after a `prepare` Swift sent later on the same callback.
                is DeviceRequest.BrowserFinish -> {
                    browser.finish(request.redirect)
                    DeviceAnswer.Value { done() }
                }
                DeviceRequest.NotificationsPermission ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(NotificationPermission.serializer(), notifications.permission())
                    }
                // The system's question is asked from the activity in front.
                DeviceRequest.NotificationsRequestPermission ->
                    DeviceAnswer.Value(dispatcher = Dispatchers.Main) {
                        BridgeJson.encodeToJsonElement(NotificationPermission.serializer(), notifications.requestPermission())
                    }
                is DeviceRequest.NotificationsRegister ->
                    DeviceAnswer.Value {
                        notifications.register(request.categories)
                        done()
                    }
                is DeviceRequest.NotificationsSchedule ->
                    DeviceAnswer.Value {
                        notifications.schedule(request.request)
                        done()
                    }
                is DeviceRequest.NotificationsCancel ->
                    DeviceAnswer.Value {
                        notifications.cancel(request.id)
                        done()
                    }
                is DeviceRequest.NotificationsCancelThread ->
                    DeviceAnswer.Value {
                        notifications.cancelThread(request.thread)
                        done()
                    }
                DeviceRequest.NotificationsCancelAll ->
                    DeviceAnswer.Value {
                        notifications.cancelAll()
                        done()
                    }
                // Android draws no number on an app's icon: a launcher shows a
                // dot for the notifications themselves.
                is DeviceRequest.NotificationsBadge -> DeviceAnswer.Value { done() }
                is DeviceRequest.NotificationsPending ->
                    DeviceAnswer.Value {
                        BridgeJson.encodeToJsonElement(ListSerializer(String.serializer()), notifications.pending(request.prefix))
                    }
                DeviceRequest.NotificationsResponses ->
                    DeviceAnswer.Chunks(values = notifications.responses().map { BridgeJson.encodeToJsonElement(NotificationResponse.serializer(), it) })
            }
        }
    }
}
