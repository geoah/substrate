// The kit's Android core (docs/plans/app-kit.md §5.3): the Swift libraries
// as one shared library (SubstrateAndroid, from apps/kit/Package.swift) with
// the bridge's Kotlin and the values generated from bridge.golden.json.
plugins {
    id("substrate.android.library")
    id("substrate.swift")
}

apply(plugin = "org.jetbrains.kotlin.plugin.serialization")

android {
    namespace = "dev.reamde.substrate.kit"
}

substrateSwift {
    packageDir.set(layout.projectDirectory.dir("../.."))
    product.set("SubstrateAndroid")
}

// The round trip's middle leg reads the fixtures Swift wrote and writes
// Kotlin's encoding of them where android.sh hands it back to Swift.
tasks.withType<Test>().configureEach {
    systemProperty("substrate.roundtrip.fixtures", layout.projectDirectory.file("../fixtures/roundtrip.json").asFile.path)
    systemProperty("substrate.roundtrip.out", layout.buildDirectory.file("roundtrip/kotlin.json").get().asFile.path)
}

dependencies {
    api(libs.serialization.json)
    api(libs.coroutines.android)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.coroutines.test)
}
