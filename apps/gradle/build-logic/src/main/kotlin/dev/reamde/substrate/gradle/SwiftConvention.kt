package dev.reamde.substrate.gradle

import com.android.build.api.variant.AndroidComponentsExtension
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.provider.Property
import org.gradle.api.services.BuildService
import org.gradle.api.services.BuildServiceParameters
import org.gradle.kotlin.dsl.create
import org.gradle.kotlin.dsl.register

/**
 * What a module's Swift is: one SwiftPM package and the dynamic library
 * product of it that carries SubstrateBridge (`System.loadLibrary`'s name).
 */
abstract class SwiftExtension {
    /** The package's folder, holding its Package.swift. */
    abstract val packageDir: DirectoryProperty

    /** The `.library(type: .dynamic)` product to build. */
    abstract val product: Property<String>
}

/** Holds Swift builds to one at a time (`maxParallelUsages`). */
abstract class SwiftBuilds : BuildService<BuildServiceParameters.None>

/**
 * Cross-compiles a SwiftPM product for every ABI in `substrate.abis` and
 * hands AGP the libraries as generated jniLibs and the packages' resources
 * as generated assets, per variant: debug builds Swift's debug
 * configuration, every other variant its release one.
 */
class SwiftConvention : Plugin<Project> {
    override fun apply(project: Project) = with(project) {
        val swift = extensions.create<SwiftExtension>("substrateSwift")
        val swiftVersion = libs.pin("swift")
        val ndkVersion = libs.pin("android-ndk")
        val minSdk = libs.pin("android-minSdk")
        val abis = abis
        val mode = findProperty("substrate.swift.mode") as String? ?: "dynamic"
        val script = rootProject.layout.projectDirectory.file("swift-jnilibs.sh")
        val components = extensions.getByType(AndroidComponentsExtension::class.java)
        // One Swift build at a time across the whole build: each takes every
        // core, and two over one package race in its checkouts.
        val swiftBuilds = gradle.sharedServices.registerIfAbsent("swiftBuilds", SwiftBuilds::class.java) {
            maxParallelUsages.set(1)
        }
        components.onVariants { variant ->
            val name = variant.name.replaceFirstChar { it.uppercase() }
            val configuration = if (variant.buildType == "debug") "debug" else "release"
            val task = tasks.register<SwiftJniLibs>("swiftJniLibs$name") {
                usesService(swiftBuilds)
                val dir = swift.packageDir.get()
                sources.from(
                    dir.file("Package.swift"),
                    dir.file("Package.resolved"),
                    fileTree(dir.dir("Sources")),
                )
                packageDir.set(dir.asFile.absolutePath)
                product.set(swift.product)
                this.abis.set(abis)
                this.configuration.set(configuration)
                this.mode.set(mode)
                this.swiftVersion.set(swiftVersion)
                this.minSdk.set(minSdk)
                ndkDir.set(components.sdkComponents.sdkDirectory.map { it.dir("ndk/$ndkVersion").asFile.absolutePath })
                this.script.set(script)
                scratch.set(layout.buildDirectory.dir("swift/scratch/$configuration"))
                jniLibs.set(layout.buildDirectory.dir("swift/jniLibs/${variant.name}"))
                assets.set(layout.buildDirectory.dir("swift/assets/${variant.name}"))
            }
            variant.sources.jniLibs?.addGeneratedSourceDirectory(task, SwiftJniLibs::jniLibs)
            variant.sources.assets?.addGeneratedSourceDirectory(task, SwiftJniLibs::assets)
        }
    }
}
