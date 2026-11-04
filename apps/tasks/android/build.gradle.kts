// Substrate Tasks on Android (docs/plans/app-kit.md §10): TasksKit's roots
// in its own Swift library (SubstrateTasks, from ../TasksKit), drawn in
// Compose with kit-ui's screens and building blocks. Its ids come from
// ../app.json. A release is stamped by `mise run android:release:build tasks`
// with the `substrate.release.*` properties and signed there, never here: a
// release build of this module is unsigned, and a debug build carries the
// debug key. Its gallery test draws Tasks' fixtures (fixtures/tasks.json)
// on the JVM; `-Psubstrate.gallery=<dir>` writes each as a PNG there.
import groovy.json.JsonSlurper

plugins {
    id("substrate.android.application")
    id("substrate.swift")
}

apply(plugin = "org.jetbrains.kotlin.plugin.compose")
apply(plugin = "org.jetbrains.kotlin.plugin.serialization")

@Suppress("UNCHECKED_CAST")
val app = JsonSlurper().parse(file("../app.json")) as Map<String, Any?>
val applicationID = (app["android"] as Map<String, Any?>)["applicationID"] as String

/** A release's stamp, empty in a local build. */
fun release(name: String): String = (findProperty("substrate.release.$name") as String?)?.trim().orEmpty()

fun quoted(value: String) = "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"") + "\""

android {
    namespace = "dev.reamde.substrate.tasks"
    defaultConfig {
        applicationId = applicationID
        versionName = release("version").ifEmpty { "0.0.0-local" }
        versionCode = release("versionCode").ifEmpty { "1" }.toInt()
        buildConfigField("String", "CHANNEL", quoted(release("channel")))
        buildConfigField("String", "COMMIT", quoted(release("commit")))
        buildConfigField("String", "UPDATES_URL", quoted(release("updatesURL")))
        buildConfigField("String", "CLIENT_ID", quoted(release("clientID")))
        buildConfigField("String", "REDIRECT", quoted(release("redirect")))
        buildConfigField("String", "SITE_HOST", quoted(release("siteHost")))
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
    packaging {
        jniLibs {
            // kit-core carries the kit's own library, SubstrateAndroid, with
            // the Swift runtime it loads; this app loads SubstrateTasks, which
            // holds the kit too, over the same runtime.
            excludes += "**/libSubstrateAndroid.so"
            // The runtime both libraries load, one copy; a catch-all here
            // would win over the exclude above.
            pickFirsts += listOf("**/libswift*.so", "**/libFoundation*.so", "**/lib_FoundationICU.so", "**/libdispatch.so",
                                 "**/libBlocksRuntime.so", "**/libc++_shared.so")
        }
    }
    testOptions {
        unitTests.isIncludeAndroidResources = true
    }
}

substrateSwift {
    packageDir.set(layout.projectDirectory.dir("../TasksKit"))
    product.set("SubstrateTasks")
}

tasks.withType<Test>().configureEach {
    val fixtures = layout.projectDirectory.file("fixtures/tasks.json")
    inputs.file(fixtures)
    systemProperty("substrate.tasks.fixtures", fixtures.asFile.path)
    systemProperty("robolectric.pixelCopyRenderMode", "hardware")
    jvmArgs("--add-opens=java.base/jdk.internal.access=ALL-UNNAMED", "--add-exports=java.base/jdk.internal.access=ALL-UNNAMED")
    maxHeapSize = "2g"
    systemProperty("roborazzi.test.record", "true")
    val gallery = providers.gradleProperty("substrate.gallery")
    systemProperty("substrate.gallery", gallery.getOrElse(layout.buildDirectory.dir("gallery/android/tasks").get().asFile.path))
    if (gallery.isPresent) outputs.upToDateWhen { false }
}

dependencies {
    implementation(project(":kit-ui"))
    implementation(project(":kit-device"))
    implementation(libs.activity.compose)
    implementation(libs.compose.material.icons.extended)
    testImplementation(libs.junit)
    testImplementation(libs.robolectric)
    testImplementation(libs.roborazzi)
    testImplementation(libs.roborazzi.compose)
    testImplementation(libs.compose.ui.test.junit4)
    debugImplementation(libs.compose.ui.test.manifest)
}
