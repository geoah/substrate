package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.StartAnswer
import dev.reamde.substrate.kit.bridge.StartConfig
import java.io.IOException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** `start` answers every failure as a terminal result, and never throws out of Application.onCreate. */
class BridgeStartTest {
    private open class Steps : Bridge.StartSteps {
        var startedCalls = 0

        override fun onMainThread() = true

        override fun load() {}

        override fun config() = StartConfig("f", "en-GB", "Europe/London", "/r", "/d", "/c", 36, false)

        override fun native(config: String): String? = BridgeJson.encodeToString(StartAnswer.Serializer, StartAnswer.Started("f"))

        override fun started() {
            startedCalls++
        }
    }

    private fun code(steps: Bridge.StartSteps): BridgeFailure.Code? = (Bridge.begin(steps, "x") as? StartResult.Failed)?.failure?.code

    @Test
    fun eachStepsFailureIsTheAnswerNotAThrow() {
        assertEquals(null, code(Steps()))
        assertEquals(
            BridgeFailure.Code.swiftFailure,
            code(
                object : Steps() {
                    override fun onMainThread() = false
                }
            ),
        )
        assertEquals(
            BridgeFailure.Code.jniFailure,
            code(
                object : Steps() {
                    override fun load() = throw UnsatisfiedLinkError("no libx.so")
                }
            ),
        )
        // The resources copied out to a full disk, or an asset that will not read.
        assertEquals(
            BridgeFailure.Code.unavailable,
            code(
                object : Steps() {
                    override fun config(): StartConfig = throw IOException("No space left on device")
                }
            ),
        )
        assertEquals(
            BridgeFailure.Code.jniFailure,
            code(
                object : Steps() {
                    override fun native(config: String): String? = throw IllegalStateException("JNI")
                }
            ),
        )
        assertEquals(
            BridgeFailure.Code.jniFailure,
            code(
                object : Steps() {
                    override fun native(config: String): String? = null
                }
            ),
        )
        assertEquals(
            BridgeFailure.Code.malformed,
            code(
                object : Steps() {
                    override fun native(config: String): String? = "nonsense"
                }
            ),
        )
        assertEquals(
            BridgeFailure.Code.kotlinException,
            code(
                object : Steps() {
                    override fun started() = throw IllegalStateException("no receiver")
                }
            ),
        )
    }

    @Test
    fun swiftsRefusalIsTheAnswerAndNothingStarts() {
        val steps =
            object : Steps() {
                override fun native(config: String): String? =
                    BridgeJson.encodeToString(StartAnswer.Serializer, StartAnswer.Failed(BridgeFailure(BridgeFailure.Code.protocolMismatch, "two trees")))
            }
        val result = Bridge.begin(steps, "x")
        assertTrue(result is StartResult.Failed && result.failure.code == BridgeFailure.Code.protocolMismatch)
        assertEquals(0, steps.startedCalls)
    }
}
