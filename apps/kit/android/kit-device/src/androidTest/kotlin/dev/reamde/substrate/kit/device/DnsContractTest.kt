package dev.reamde.substrate.kit.device

import androidx.test.ext.junit.runners.AndroidJUnit4
import dev.reamde.substrate.kit.bridge.DNSAnswer
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/** TXT lookups through the system resolver, against public DNS: needs a network. */
@RunWith(AndroidJUnit4::class)
class DnsContractTest {
    @Test
    fun aRecordAndAnAbsenceAreTold() = runBlocking {
        val found = Dns().txt("_substrate.substrate.reamde.dev", 10_000)
        assertTrue("$found", found is DNSAnswer.Answered && found.rcode == 0L && found.records.isNotEmpty())
        val absent = Dns().txt("_substrate.no-such-name.example.com", 10_000)
        // NXDOMAIN, or NOERROR with no record: either is a confirmed absence in Swift.
        assertTrue("$absent", absent is DNSAnswer.Answered && absent.rcode in setOf(0L, 3L) && absent.records.isEmpty())
    }
}
