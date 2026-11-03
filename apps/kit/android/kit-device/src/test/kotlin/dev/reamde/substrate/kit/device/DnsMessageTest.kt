package dev.reamde.substrate.kit.device

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class DnsMessageTest {
    private fun name(vararg labels: String) = labels.flatMap { listOf(it.length.toByte()) + it.toByteArray().toList() } + 0

    /** A response to `_substrate.example.com TXT` with one record of two strings, the answer's name compressed. */
    private val answer: ByteArray = (
        listOf(0, 1, 0x81, 0x80, 0, 1, 0, 1, 0, 0, 0, 0).map { it.toByte() } +
            name("_substrate", "example", "com") + listOf<Byte>(0, 16, 0, 1) +
            listOf(0xc0, 0x0c, 0, 16, 0, 1, 0, 0, 0, 60).map { it.toByte() } +
            run {
                val rdata = listOf(12.toByte()) + "v=substrate1".toByteArray().toList() + listOf(3.toByte()) + "ü!".toByteArray().toList()
                listOf(0.toByte(), rdata.size.toByte()) + rdata
            }
        ).toByteArray()

    @Test
    fun readsEachRecordsCharacterStrings() {
        assertEquals(listOf(listOf("v=substrate1", "ü!")), DnsMessage.txtRecords(answer))
    }

    @Test
    fun aTruncatedMessageIsRefusedNotMisread() {
        assertThrows(IllegalArgumentException::class.java) { DnsMessage.txtRecords(answer.copyOf(answer.size - 2)) }
    }
}
