package dev.reamde.substrate.gradle

import org.gradle.api.Project
import org.gradle.api.artifacts.VersionCatalog
import org.gradle.api.artifacts.VersionCatalogsExtension
import org.gradle.kotlin.dsl.getByType

/** The one version catalog, gradle/libs.versions.toml. */
internal val Project.libs: VersionCatalog
    get() = extensions.getByType<VersionCatalogsExtension>().named("libs")

/** A pin from the catalog's [versions]; a missing one fails the build by name. */
internal fun VersionCatalog.pin(name: String): String =
    findVersion(name).orElseThrow { IllegalStateException("gradle/libs.versions.toml pins no $name") }.requiredVersion

/** The ABIs a build compiles Swift for and packages: `-Psubstrate.abis=arm64-v8a`. */
internal val Project.abis: List<String>
    get() = (findProperty("substrate.abis") as String? ?: "arm64-v8a,x86_64").split(",").map { it.trim() }.filter { it.isNotEmpty() }

/**
 * Every configuration resolves to what the module's gradle.lockfile names,
 * so a version range or a dependency's own new requirement cannot move a
 * build. `./gradlew … --write-locks` rewrites the files (`mise run
 * android:lock`).
 */
internal fun Project.lockDependencies() {
    dependencyLocking { lockAllConfigurations() }
}
