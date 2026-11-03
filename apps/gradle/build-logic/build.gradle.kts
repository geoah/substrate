plugins {
    `kotlin-dsl`
}

dependencyLocking {
    lockAllConfigurations()
}

dependencies {
    compileOnly(libs.android.gradle.plugin)
    compileOnly(libs.kotlin.gradle.plugin)
    runtimeOnly(libs.android.gradle.plugin)
    runtimeOnly(libs.kotlin.serialization.plugin)
}

gradlePlugin {
    plugins {
        register("androidLibrary") {
            id = "substrate.android.library"
            implementationClass = "dev.reamde.substrate.gradle.AndroidLibraryConvention"
        }
        register("androidApplication") {
            id = "substrate.android.application"
            implementationClass = "dev.reamde.substrate.gradle.AndroidApplicationConvention"
        }
        register("swift") {
            id = "substrate.swift"
            implementationClass = "dev.reamde.substrate.gradle.SwiftConvention"
        }
    }
}
