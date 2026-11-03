// The shared screens drawn in Compose (docs/plans/app-kit.md §5.6, §7.4):
// one composable per SubstrateUI view, drawing the same presentation and
// sending the same commands, in Material 3, with the design tokens Swift
// writes (KitTokens.kt). Its gallery test draws every fixture of
// ../fixtures/screens.json on the JVM; `-Psubstrate.gallery=<dir>` writes
// each as a PNG there (`mise run apps:gallery`).
plugins {
    id("substrate.android.library")
}

apply(plugin = "org.jetbrains.kotlin.plugin.compose")

android {
    namespace = "dev.reamde.substrate.kit.ui"
    buildFeatures {
        compose = true
    }
    testOptions {
        unitTests.isIncludeAndroidResources = true
    }
}

tasks.withType<Test>().configureEach {
    val screens = layout.projectDirectory.file("../fixtures/screens.json")
    inputs.file(screens)
    systemProperty("substrate.screens.fixtures", screens.asFile.path)
    systemProperty("robolectric.pixelCopyRenderMode", "hardware")
    // Robolectric reaches a FileDescriptor's internals, which JDK 21 keeps
    // closed unless the test's JVM opens them.
    jvmArgs("--add-opens=java.base/jdk.internal.access=ALL-UNNAMED", "--add-exports=java.base/jdk.internal.access=ALL-UNNAMED")
    maxHeapSize = "2g"
    // Every run draws every fixture, so a composable that throws fails the
    // build; the pictures are only kept where -Psubstrate.gallery says.
    systemProperty("roborazzi.test.record", "true")
    val gallery = providers.gradleProperty("substrate.gallery")
    systemProperty("substrate.gallery", gallery.getOrElse(layout.buildDirectory.dir("gallery/android").get().asFile.path))
    if (gallery.isPresent) outputs.upToDateWhen { false }
}

dependencies {
    api(project(":kit-core"))
    api(platform(libs.compose.bom))
    api(libs.compose.material3)
    api(libs.compose.ui)
    implementation(libs.compose.material.icons.extended)
    testImplementation(libs.junit)
    testImplementation(libs.robolectric)
    testImplementation(libs.roborazzi)
    testImplementation(libs.roborazzi.compose)
    testImplementation(libs.compose.ui.test.junit4)
    // The empty activity a compose test rule draws in, for the gallery test.
    debugImplementation(libs.compose.ui.test.manifest)
}
