package dev.reamde.substrate.gradle

import javax.xml.parsers.DocumentBuilderFactory
import org.gradle.api.GradleException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.w3c.dom.Element

/**
 * A release claims its stamped callback in its manifest, as a verified App Link that kit-device's `SignInCallback` takes and nothing else does; a callback a
 * release may not claim is refused by name, by the check every release variant waits on and by the manifest's writer alike.
 */
class ReleaseSignInTest {
    private val android = "http://schemas.android.com/apk/res/android"

    private fun parse(manifest: String): Element =
        DocumentBuilderFactory.newInstance().apply { isNamespaceAware = true }.newDocumentBuilder().parse(manifest.byteInputStream()).documentElement

    private fun Element.children(name: String): List<Element> =
        (0 until childNodes.length).map { childNodes.item(it) }.filterIsInstance<Element>().filter { it.tagName == name }

    private fun Element.android(name: String): String = getAttributeNS(android, name)

    @Test
    fun aReleaseClaimsItsCallbackWithAVerifiedLinkForItsHostAndPathAlone() {
        val manifest = parse(callbackManifest("https://Apps.Example.com/apps/tasks/android/callback"))
        val activity = manifest.children("application").single().children("activity").single()
        assertEquals("dev.reamde.substrate.kit.device.SignInCallback", activity.android("name"))
        assertEquals("true", activity.android("exported"))
        assertEquals("", activity.android("taskAffinity"))
        assertTrue(activity.hasAttributeNS(android, "taskAffinity"))
        val filter = activity.children("intent-filter").single()
        assertEquals("true", filter.android("autoVerify"))
        assertEquals(listOf("android.intent.action.VIEW"), filter.children("action").map { it.android("name") })
        assertEquals(
            setOf("android.intent.category.DEFAULT", "android.intent.category.BROWSABLE"),
            filter.children("category").map { it.android("name") }.toSet(),
        )
        val data = filter.children("data").single()
        assertEquals("https", data.android("scheme"))
        // Android matches a host as it is written, and every browser writes it in lower case.
        assertEquals("apps.example.com", data.android("host"))
        assertEquals("/apps/tasks/android/callback", data.android("path"))
        assertTrue(!data.hasAttributeNS(android, "pathPrefix") && !data.hasAttributeNS(android, "pathPattern") && !data.hasAttributeNS(android, "port"))
    }

    @Test
    fun aPathIsWrittenAsTheCharactersItNames() {
        val data = parse(callbackManifest("https://apps.example.com/a&b/%C3%A9")).getElementsByTagName("data").item(0) as Element
        assertEquals("/a&b/é", data.android("path"))
    }

    @Test
    fun aCallbackAReleaseMayNotClaimIsRefusedByTheCheckAndTheManifestAlike() {
        for (redirect in
            listOf(
                "",
                "http://apps.example.com/apps/tasks/android/callback",
                "https:///apps/tasks/android/callback",
                "https://apps.example.com",
                "https://apps.example.com/",
                "https://apps.example.com/callback?x=1",
                "https://apps.example.com/callback#x",
                "https://someone@apps.example.com/callback",
                "https://apps.example.com:8443/callback",
                "not a url",
            )) {
            assertEquals(redirect, 1, releaseSignInProblems("https://apps.example.com/apps/tasks/client.json", redirect).size)
            try {
                callbackManifest(redirect)
                fail("a manifest claimed \"$redirect\"")
            } catch (_: GradleException) {}
        }
        assertTrue(releaseSignInProblems("https://apps.example.com/apps/tasks/client.json", "https://apps.example.com/apps/tasks/android/callback").isEmpty())
        assertEquals(1, releaseSignInProblems("http://apps.example.com/client.json", "https://apps.example.com/apps/tasks/android/callback").size)
    }
}
