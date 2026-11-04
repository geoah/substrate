// The bridge's runtime check: an app that starts the bridge from its
// Application, as every app will, and runs BridgeChecks against the Swift
// library on a device. `mise run android:smoke` installs its release build,
// minified as a release is, and reads the results from the log; its
// instrumented test runs the same checks against the debug build.
plugins {
    id("substrate.android.application")
}

android {
    namespace = "dev.reamde.substrate.bridgecheck"
    defaultConfig {
        applicationId = "dev.reamde.substrate.bridgecheck"
        versionCode = 1
        versionName = "0.0.0"
    }
    buildTypes {
        getByName("release") {
            // Never published: signed with the debug key so it installs.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

dependencies {
    implementation(project(":kit-core"))
    implementation(project(":kit-device"))
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(libs.androidx.test.ext.junit)
}
