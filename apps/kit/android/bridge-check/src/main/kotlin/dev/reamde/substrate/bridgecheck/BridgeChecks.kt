package dev.reamde.substrate.bridgecheck

import android.content.Context
import dev.reamde.substrate.kit.Bridge
import dev.reamde.substrate.kit.BridgeHandle
import dev.reamde.substrate.kit.StartResult
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.CheckCommand
import dev.reamde.substrate.kit.bridge.CheckPresentation
import dev.reamde.substrate.kit.bridge.CheckResult
import dev.reamde.substrate.kit.bridge.CheckRow
import dev.reamde.substrate.kit.bridge.CheckTransport
import dev.reamde.substrate.kit.bridge.Counter
import dev.reamde.substrate.kit.bridge.ProtocolFingerprint
import dev.reamde.substrate.kit.bridge.RouteCommand
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeout

/**
 * The bridge's runtime checks (docs/plans/app-kit.md §8): what presenter tests and screenshots cannot catch, run against the Swift library on a device. Each
 * check is one line: its name, PASS or FAIL, what it saw, and how long it took.
 */
class BridgeChecks(private val context: Context, private val emit: (String) -> Unit) {
    data class Result(val name: String, val passed: Boolean, val detail: String, val millis: Double)

    private val results = mutableListOf<Result>()

    suspend fun runAll(): List<Result> {
        val server = LocalServer()
        try {
            check("start") {
                val start = CheckApplication.start
                require(start is StartResult.Started && start.fingerprint == ProtocolFingerprint.VALUE) { "$start" }
                "started in ${"%.1f".format(CheckApplication.startMillis)} ms, fingerprint ${start.fingerprint.take(12)}"
            }
            check("round trip") { roundTrip() }
            check("unicode") {
                val text = "🧪 emoji 😀, עברית, é, tab\t, quote\", nul\u0000, 中文"
                val echoed = single(CheckCommand.Echo(text))
                require(echoed is CheckResult.Ok && echoed.detail == text) { "came back as $echoed" }
                "${text.length} UTF-16 units back unchanged"
            }
            check("main actor") { ok(single(CheckCommand.MainActor)) }
            check("resource") { ok(single(CheckCommand.Resource)) }
            check("runtime root") {
                val handle = Bridge.open("counter").getOrThrow()
                try {
                    val presentations = handle.presentations(Counter.Presentation.serializer())
                    withTimeout(5_000) { presentations.first { it.count == 0L } }
                    val add = BridgeJson.encodeToJsonElement(Counter.Command.Serializer, Counter.Command.Add(1))
                    handle.send(RouteCommand.serializer(), RouteCommand(emptyList(), add)).getOrThrow()
                    val shown = withTimeout(5_000) { presentations.first { it.count == 1L } }
                    "a Runtime opened as a root with no glue: count ${shown.count}, \"${shown.words}\""
                } finally {
                    handle.close()
                }
            }
            check("idle root x50") { idle(50) }
            check("open and close x200") { openClose(200) }
            check("cancellation races x100") { cancellationRaces(100) }
            check("slow consumer") { slowConsumer(2000) }
            check("large presentation") {
                withHandle { handle ->
                    val began = System.nanoTime()
                    handle.send(CheckCommand.serializer(), CheckCommand.Burst(1, 4 * 1024 * 1024)).getOrThrow()
                    val shown = handle.presentations(CheckPresentation.serializer()).first { it.step >= 2 }
                    "4 MiB presentation in ${ms(began)} ms (${shown.padding?.length} chars)"
                }
            }
            check("keyed list") { keyedList() }
            check("large response") {
                val big = ok(single(CheckCommand.Fetch("${server.origin}/bytes?n=8388608", CheckTransport.bridge)))
                val inline = ok(single(CheckCommand.Fetch("${server.origin}/bytes?n=102400", CheckTransport.bridge)))
                require(big.startsWith("200 8388608 bytes") && inline.startsWith("200 102400 bytes")) { "$big; $inline" }
                "$big (file), $inline (inline)"
            }
            check("line stream") {
                val lines = ok(single(CheckCommand.Lines("${server.origin}/lines?n=2000", 0, 4096)))
                require(lines.startsWith("200 2000 lines")) { lines }
                lines
            }
            check("line stream overflow") {
                val result = single(CheckCommand.Lines("${server.origin}/lines?n=50000", 5, 8), timeout = 60_000)
                require(result is CheckResult.Failed && result.detail.contains("overflow")) { "$result" }
                result.detail
            }
            check("redirect refused") {
                val fetched = ok(single(CheckCommand.Fetch("${server.origin}/redirect", CheckTransport.bridge)))
                val streamed = ok(single(CheckCommand.Lines("${server.origin}/redirect", 0, 16)))
                require(fetched.startsWith("302") && streamed.startsWith("302") && server.landed.get() == 0) {
                    "$fetched; $streamed; landed ${server.landed.get()}"
                }
                "fetch $fetched, stream $streamed, nothing landed"
            }
            check("secure store") {
                val written = ok(single(CheckCommand.Secure("check", "secret ✓")))
                ok(single(CheckCommand.SecureDelete("check")))
                val gone = ok(single(CheckCommand.Secure("check", null)))
                require(written == "found secret ✓" && gone == "missing") { "$written; $gone" }
                "$written, then $gone"
            }
            check("transports, loopback") { compare("${server.origin}/bytes?n=1024", 20) }
            check("transports, network") { compare("https://example.com/", 5) }
            check("dns") {
                val found = ok(single(CheckCommand.Txt("_substrate.substrate.reamde.dev")))
                val absent = ok(single(CheckCommand.Txt("_substrate.nothing-here.example.com")))
                "$found; $absent"
            }
            check("connectivity") { ok(single(CheckCommand.Connectivity)) }
            check("stats") {
                val stats = Bridge.stats()
                require(stats.deliveryFailures == 0L && stats.open == 0L) { "$stats" }
                "$stats"
            }
        } finally {
            server.close()
        }
        return results
    }

    private suspend fun check(name: String, body: suspend () -> String) {
        val began = System.nanoTime()
        val result =
            try {
                Result(name, true, body(), ms(began))
            } catch (failure: Throwable) {
                Result(name, false, "${failure::class.java.simpleName}: ${failure.message}", ms(began))
            }
        results += result
        emit("${if (result.passed) "PASS" else "FAIL"} ${result.name} (${"%.1f".format(result.millis)} ms): ${result.detail}")
    }

    private fun ms(began: Long) = (System.nanoTime() - began) / 1e6

    private fun ok(result: CheckResult): String =
        when (result) {
            is CheckResult.Ok -> result.detail
            is CheckResult.Failed -> throw IllegalStateException("${result.name}: ${result.detail}")
        }

    private suspend fun <T> withHandle(body: suspend (BridgeHandle) -> T): T {
        val handle = Bridge.open("check").getOrThrow()
        try {
            return body(handle)
        } finally {
            handle.close()
        }
    }

    /** One command on a fresh handle, and the presentation it answers with. */
    private suspend fun single(command: CheckCommand, timeout: Long = 30_000): CheckResult = withHandle { handle ->
        handle.send(CheckCommand.serializer(), command).getOrThrow()
        withTimeout(timeout) { handle.presentations(CheckPresentation.serializer()).first { it.step >= 1 } }.result ?: throw IllegalStateException("no result")
    }

    private suspend fun roundTrip(): String = withHandle { handle ->
        val presentations = handle.presentations(CheckPresentation.serializer())
        val times = mutableListOf<Double>()
        for (step in 1..51) {
            val began = System.nanoTime()
            handle.send(CheckCommand.serializer(), CheckCommand.Echo("ping $step")).getOrThrow()
            withTimeout(5_000) { presentations.first { it.step >= step } }
            if (step > 1) times += ms(began)
        }
        times.sort()
        "50 round trips: median ${"%.2f".format(times[times.size / 2])} ms, p95 ${"%.2f".format(times[(times.size * 95) / 100])} ms, " +
            "min ${"%.2f".format(times.first())} ms, max ${"%.2f".format(times.last())} ms"
    }

    private suspend fun openClose(count: Int): String {
        val baseline = Bridge.stats().open
        val began = System.nanoTime()
        repeat(count) { index ->
            val handle = Bridge.open("check").getOrThrow()
            handle.send(CheckCommand.serializer(), CheckCommand.Echo("$index")).getOrThrow()
            withTimeout(5_000) { handle.presentations(CheckPresentation.serializer()).first { it.step >= 1 } }
            handle.close().getOrThrow()
            handle.close().getOrThrow()
            withTimeout(5_000) { handle.state.first { it.closed } }
        }
        val open = Bridge.stats().open
        require(open == baseline) { "$open handles open after, $baseline before" }
        return "$count cycles, ${"%.2f".format(ms(began) / count)} ms each, every handle closed and closing twice accepted"
    }

    /** A root that presents once, as it opens, and never again: that one presentation must arrive. */
    private suspend fun idle(count: Int): String {
        repeat(count) {
            val handle = Bridge.open("idle").getOrThrow()
            try {
                withTimeout(5_000) { handle.presentations(CheckPresentation.serializer()).first { it.step == 1L } }
            } finally {
                handle.close()
            }
        }
        return "$count opens of a root that presents only as it opens; each showed it"
    }

    private suspend fun cancellationRaces(count: Int): String {
        val before = Bridge.stats()
        repeat(count) { index ->
            val handle = Bridge.open("check").getOrThrow()
            handle.send(CheckCommand.serializer(), CheckCommand.Hang).getOrThrow()
            if (index % 2 == 0) delay(1)
            handle.close().getOrThrow()
            // A command after the close is refused, never delivered to the next root.
            require(handle.send(CheckCommand.serializer(), CheckCommand.Echo("late")).isFailure) { "a command after close was accepted" }
            withTimeout(5_000) { handle.state.first { it.closed } }
        }
        withTimeout(5_000) { while (Bridge.runningCalls > 0) delay(10) }
        val after = Bridge.stats()
        require(after.open == before.open) { "${after.open} handles open" }
        return "$count closes during a call: ${after.cancelled - before.cancelled} calls answered cancelled, " +
            "${after.dropped - before.dropped} late messages dropped, none left running"
    }

    private suspend fun slowConsumer(count: Int): String = withHandle { handle ->
        var seen = 0
        var last = 0L
        coroutineScope {
            val reader = launch {
                handle.presentations(CheckPresentation.serializer()).collect {
                    seen++
                    last = it.step
                    delay(2)
                }
            }
            handle.send(CheckCommand.serializer(), CheckCommand.Burst(count.toLong(), 64)).getOrThrow()
            handle.send(CheckCommand.serializer(), CheckCommand.Echo("end")).getOrThrow()
            val final = count + 2L
            withTimeout(30_000) { handle.presentations(CheckPresentation.serializer()).first { it.step >= final } }
            withTimeout(5_000) { while (last < final) delay(5) }
            reader.cancel()
        }
        require(last == count + 2L) { "the reader ended on step $last" }
        val coalesced = Bridge.stats().coalesced
        "$count presentations, a 2 ms reader saw $seen and ended on the newest; $coalesced coalesced in Swift so far"
    }

    private suspend fun keyedList(): String = withHandle { handle ->
        suspend fun rows(): List<CheckRow> =
            withTimeout(10_000) {
                handle.state.first { it.lists["rows"] != null }.lists.getValue("rows").map { BridgeJson.decodeFromJsonElement(CheckRow.serializer(), it.value) }
            }
        handle.send(CheckCommand.serializer(), CheckCommand.Rows(1000)).getOrThrow()
        withTimeout(10_000) { handle.state.first { it.lists["rows"]?.size == 1000 } }
        handle.send(CheckCommand.serializer(), CheckCommand.ChangeRow("row-5", "changed")).getOrThrow()
        withTimeout(10_000) {
            handle.state.first { snapshot -> snapshot.lists["rows"]?.getOrNull(5)?.value?.toString()?.contains("changed") == true }
        }
        handle.send(CheckCommand.serializer(), CheckCommand.RemoveRow("row-0")).getOrThrow()
        withTimeout(10_000) { handle.state.first { it.lists["rows"]?.size == 999 } }
        val final = rows()
        require(final.first().id == "row-1" && final[4].text == "changed") { "rows are ${final.take(5)}" }
        "1000 rows whole, one changed and one removed by delta, in order"
    }

    private suspend fun compare(url: String, times: Int): String {
        fun median(values: List<Double>) = values.sorted()[values.size / 2]
        val bridge = mutableListOf<Double>()
        val session = mutableListOf<Double>()
        repeat(times + 1) { index ->
            val viaBridge = single(CheckCommand.Fetch(url, CheckTransport.bridge))
            val viaSession = single(CheckCommand.Fetch(url, CheckTransport.urlSession))
            ok(viaBridge)
            ok(viaSession)
            if (index > 0) {
                bridge += (viaBridge as CheckResult.Ok).millis
                session += (viaSession as CheckResult.Ok).millis
            }
        }
        return "$times GETs of $url: OkHttp through the bridge median ${"%.1f".format(median(bridge))} ms, " +
            "URLSession median ${"%.1f".format(median(session))} ms"
    }
}
