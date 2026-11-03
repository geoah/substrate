// The device services in Kotlin (docs/plans/app-kit.md §5.4): what answers
// SubstrateBridge's device calls on Android. The one module of the kit
// whose Kotlin may touch an Android service (`mise run lint:apps`).
plugins {
    id("substrate.android.library")
}

apply(plugin = "org.jetbrains.kotlin.plugin.serialization")

android {
    namespace = "dev.reamde.substrate.kit.device"
}

dependencies {
    api(project(":kit-core"))
    implementation(libs.okhttp)
    implementation(libs.tink.android)
    implementation(libs.datastore.core)
    testImplementation(libs.junit)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.ext.junit)
    androidTestImplementation(libs.okhttp.mockwebserver)
    androidTestImplementation(libs.coroutines.test)
}
