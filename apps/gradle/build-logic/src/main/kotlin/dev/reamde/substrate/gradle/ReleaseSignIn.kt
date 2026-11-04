package dev.reamde.substrate.gradle

import com.android.build.api.variant.AndroidComponentsExtension
import java.net.URI
import org.gradle.api.DefaultTask
import org.gradle.api.GradleException
import org.gradle.api.Project
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction
import org.gradle.kotlin.dsl.register

/**
 * A release's sign-in, held and claimed. A release-type build of an app that signs in is refused unless it is stamped with an https client id and the https
 * callback it claims (`-Psubstrate.release.clientID`, `-Psubstrate.release.redirect`): a release signs in through that callback alone, never through loopback
 * (docs/plans/app-kit.md §5.4), so one built without them could not sign in at all. Every release variant's `pre<Variant>Build` waits on the check, so no task
 * of the variant runs first. The callback is claimed in a release's manifest alone ([callbackManifest]): kit-device's `SignInCallback`, with a verified App
 * Link for the stamped host and path, which Android hands the app once the site's `assetlinks.json` names it. A debug build signs in through loopback and
 * claims nothing.
 */
fun Project.requireReleaseSignIn(clientID: String, redirect: String) {
    val check =
        tasks.register<CheckReleaseSignIn>("checkReleaseSignIn") {
            this.clientID.set(clientID)
            this.redirect.set(redirect)
        }
    val claim =
        tasks.register<ReleaseCallbackManifest>("substrateReleaseCallback") {
            this.redirect.set(redirect)
            output.set(layout.buildDirectory.file("generated/substrate/release-callback/AndroidManifest.xml"))
        }
    val components = extensions.getByType(AndroidComponentsExtension::class.java)
    components.onVariants(components.selector().withBuildType("release")) { variant ->
        val pre = "pre${variant.name.replaceFirstChar { it.uppercase() }}Build"
        tasks.matching { it.name == pre }.configureEach { dependsOn(check) }
        variant.sources.manifests.addGeneratedManifestFile(claim, ReleaseCallbackManifest::output)
    }
}

/** The release stamp's sign-in half, held to what `TasksOpening` accepts and an App Link can claim. */
abstract class CheckReleaseSignIn : DefaultTask() {
    @get:Input abstract val clientID: Property<String>

    @get:Input abstract val redirect: Property<String>

    @TaskAction
    fun check() {
        val problems = releaseSignInProblems(clientID.get(), redirect.get())
        if (problems.isNotEmpty()) {
            throw GradleException(
                "A release signs in only through its claimed https callback, so it is built with both halves of the stamp: " +
                    problems.joinToString("; ") +
                    ". `mise run android:release:build <app>` passes them; a local build is a debug one."
            )
        }
    }
}

/** Writes the release's claim on its callback ([callbackManifest]), a manifest of the release variants alone. */
abstract class ReleaseCallbackManifest : DefaultTask() {
    @get:Input abstract val redirect: Property<String>

    @get:OutputFile abstract val output: RegularFileProperty

    @TaskAction
    fun write() {
        output.get().asFile.apply { parentFile.mkdirs() }.writeText(callbackManifest(redirect.get()))
    }
}

/** What is wrong with a release's client id and callback, empty where nothing is. */
fun releaseSignInProblems(clientID: String, redirect: String): List<String> {
    val problems = mutableListOf<String>()
    val client = runCatching { URI(clientID.trim()) }.getOrNull()
    if (client?.scheme?.lowercase() != "https" || client.host.isNullOrEmpty()) {
        problems += "substrate.release.clientID is not an https URL (\"$clientID\")"
    }
    if (claimedCallback(redirect) == null) {
        problems += "substrate.release.redirect is not an https callback with a host and a path, and no port (\"$redirect\")"
    }
    return problems
}

/**
 * [redirect] where it is a callback a release may claim: https, with a host and a path, and no query, fragment or user, as `TasksOpening` holds it, and no
 * port, as the site whose `assetlinks.json` Android verifies the link against is served at its host's root.
 */
internal fun claimedCallback(redirect: String): URI? {
    val callback = runCatching { URI(redirect.trim()) }.getOrNull() ?: return null
    if (callback.scheme?.lowercase() != "https" || callback.host.isNullOrEmpty() || (callback.path?.length ?: 0) <= 1) return null
    if (callback.rawQuery != null || callback.rawFragment != null || callback.rawUserInfo != null || callback.port != -1) return null
    return callback
}

/**
 * The manifest a release claims [redirect] with: kit-device's `SignInCallback`, exported as an App Link's activity must be, in a task of its own so it never
 * sits on the app's, with a verified (`autoVerify`) filter for the callback's host and path alone. A redirect a release may not claim fails the build by name.
 */
fun callbackManifest(redirect: String): String {
    val callback =
        claimedCallback(redirect)
            ?: throw GradleException("substrate.release.redirect is not an https callback with a host and a path, and no port (\"$redirect\")")
    return """
        |<?xml version="1.0" encoding="utf-8"?>
        |<!-- Generated by build-logic's ReleaseCallbackManifest from the release's stamp; do not edit. -->
        |<manifest xmlns:android="http://schemas.android.com/apk/res/android">
        |    <application>
        |        <activity
        |            android:name="dev.reamde.substrate.kit.device.SignInCallback"
        |            android:exported="true"
        |            android:excludeFromRecents="true"
        |            android:noHistory="true"
        |            android:taskAffinity=""
        |            android:theme="@android:style/Theme.Translucent.NoTitleBar">
        |            <intent-filter android:autoVerify="true">
        |                <action android:name="android.intent.action.VIEW" />
        |                <category android:name="android.intent.category.DEFAULT" />
        |                <category android:name="android.intent.category.BROWSABLE" />
        |                <data android:scheme="https" android:host="${xml(callback.host.lowercase())}" android:path="${xml(callback.path)}" />
        |            </intent-filter>
        |        </activity>
        |    </application>
        |</manifest>
        |"""
        .trimMargin()
}

private fun xml(text: String) = text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("\"", "&quot;")
