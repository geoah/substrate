package dev.reamde.substrate.gradle

import javax.inject.Inject
import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.InputFiles
import org.gradle.api.tasks.Internal
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction
import org.gradle.process.ExecOperations

/**
 * One SwiftPM product, built by swift-jnilibs.sh once per ABI. The sources of the package and of every local package it depends on are inputs, and so is the
 * script, so an unchanged tree skips the build and any change to what goes into the library runs it; the toolchain is not, and the script refuses one that is
 * not exactly [swiftVersion].
 */
@CacheableTask
abstract class SwiftJniLibs : DefaultTask() {
    @get:InputFiles @get:PathSensitive(PathSensitivity.RELATIVE) abstract val sources: ConfigurableFileCollection

    @get:Input abstract val packageDir: Property<String>

    @get:Input abstract val product: Property<String>

    @get:Input abstract val abis: ListProperty<String>

    @get:Input abstract val configuration: Property<String>

    /** dynamic: the Swift runtime and Foundation as the SDK ships them, one .so each; static: linked into the product. */
    @get:Input abstract val mode: Property<String>

    @get:Input abstract val swiftVersion: Property<String>

    @get:Input abstract val minSdk: Property<String>

    @get:Input abstract val ndkDir: Property<String>

    @get:InputFile @get:PathSensitive(PathSensitivity.NONE) abstract val script: RegularFileProperty

    @get:Internal abstract val scratch: DirectoryProperty

    @get:OutputDirectory abstract val jniLibs: DirectoryProperty

    @get:OutputDirectory abstract val assets: DirectoryProperty

    @get:Inject abstract val exec: ExecOperations

    @TaskAction
    fun build() {
        for (abi in abis.get()) {
            exec.exec {
                commandLine(
                    script.get().asFile.path,
                    abi,
                    jniLibs.get().asFile.path,
                    assets.get().asFile.path,
                )
                environment("SWIFT_PACKAGE", packageDir.get())
                environment("SWIFT_PRODUCT", product.get())
                environment("SWIFT_CONFIGURATION", configuration.get())
                environment("SWIFT_MODE", mode.get())
                environment("SWIFT_VERSION", swiftVersion.get())
                environment("SWIFT_MIN_SDK", minSdk.get())
                environment("SWIFT_SCRATCH", scratch.get().dir(abi).asFile.path)
                environment("ANDROID_NDK_HOME", ndkDir.get())
            }
        }
    }
}
