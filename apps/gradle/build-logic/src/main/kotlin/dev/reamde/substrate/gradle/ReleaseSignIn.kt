package dev.reamde.substrate.gradle

import com.android.build.api.variant.AndroidComponentsExtension
import java.net.URI
import org.gradle.api.DefaultTask
import org.gradle.api.GradleException
import org.gradle.api.Project
import org.gradle.api.provider.Property
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.TaskAction
import org.gradle.kotlin.dsl.register

/**
 * Refuses a release-type build of an app that signs in unless it is stamped with an https client id and the https callback it claims
 * (`-Psubstrate.release.clientID`, `-Psubstrate.release.redirect`): a release signs in through that callback alone, never through loopback
 * (docs/plans/app-kit.md §5.4), so one built without them could not sign in at all. Every release variant's `pre<Variant>Build` waits on the check, so no task
 * of the variant runs first.
 */
fun Project.requireReleaseSignIn(clientID: String, redirect: String) {
    val check =
        tasks.register<CheckReleaseSignIn>("checkReleaseSignIn") {
            this.clientID.set(clientID)
            this.redirect.set(redirect)
        }
    val components = extensions.getByType(AndroidComponentsExtension::class.java)
    components.onVariants(components.selector().withBuildType("release")) { variant ->
        val pre = "pre${variant.name.replaceFirstChar { it.uppercase() }}Build"
        tasks.matching { it.name == pre }.configureEach { dependsOn(check) }
    }
}

/** The release stamp's sign-in half, held to what `TasksOpening` accepts. */
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

/** What is wrong with a release's client id and callback, empty where nothing is. */
fun releaseSignInProblems(clientID: String, redirect: String): List<String> {
    val problems = mutableListOf<String>()
    val client = runCatching { URI(clientID.trim()) }.getOrNull()
    if (client?.scheme?.lowercase() != "https" || client.host.isNullOrEmpty()) {
        problems += "substrate.release.clientID is not an https URL (\"$clientID\")"
    }
    val callback = runCatching { URI(redirect.trim()) }.getOrNull()
    if (
        callback?.scheme?.lowercase() != "https" ||
            callback.host.isNullOrEmpty() ||
            (callback.path?.length ?: 0) <= 1 ||
            callback.rawQuery != null ||
            callback.rawFragment != null ||
            callback.rawUserInfo != null
    ) {
        problems += "substrate.release.redirect is not an https callback with a host and a path (\"$redirect\")"
    }
    return problems
}
