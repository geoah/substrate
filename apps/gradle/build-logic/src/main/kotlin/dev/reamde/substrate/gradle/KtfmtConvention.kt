package dev.reamde.substrate.gradle

import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.api.attributes.Bundling
import org.gradle.api.attributes.Usage
import org.gradle.api.tasks.JavaExec
import org.gradle.kotlin.dsl.named
import org.gradle.kotlin.dsl.register
import org.gradle.process.CommandLineArgumentProvider

/**
 * ktfmt, the apps' Kotlin formatter, as one task: `./gradlew :ktfmt --args='<files>'`.
 * `mise run fmt:kotlin` runs it over the files in place and `fmt:kotlin:check`
 * over a copy, then diffs (.mise/appsfmt.sh, which also names the files), so the
 * writer and the checker are this one version and style. The width and indent are
 * apps/.editorconfig's. The formatter resolves through a locked configuration,
 * so the dependency verification metadata holds its bytes as it holds the build's:
 * the shadowed variant, ktfmt's one self-contained jar, so one checksum covers
 * the Kotlin compiler it parses with as well.
 */
class KtfmtConvention : Plugin<Project> {
    override fun apply(project: Project) = with(project) {
        val ktfmt = configurations.create("ktfmt") {
            isCanBeConsumed = false
            isCanBeResolved = true
            attributes {
                attribute(Usage.USAGE_ATTRIBUTE, objects.named(Usage.JAVA_RUNTIME))
                attribute(Bundling.BUNDLING_ATTRIBUTE, objects.named(Bundling.SHADOWED))
            }
        }
        dependencies.add(ktfmt.name, libs.findLibrary("ktfmt").orElseThrow { IllegalStateException("gradle/libs.versions.toml lists no ktfmt") })
        tasks.register<JavaExec>("ktfmt") {
            description = "Format the Kotlin files named by --args in place with ktfmt"
            classpath = ktfmt
            mainClass.set("com.facebook.ktfmt.cli.Main")
            // A provider, not args(): `--args` replaces the task's args, so the
            // style set there would be dropped from every run that names files.
            argumentProviders.add(CommandLineArgumentProvider { listOf("--kotlinlang-style", "--enable-editorconfig", "--quiet") })
        }
        lockDependencies()
    }
}
