// The kit's Gallery app (docs/plans/app-kit.md §7.4): every shared screen in
// every state, live on a device. Its fixtures are made by the screens' own
// presenters in Swift, on the device, and reach Compose through the bridge
// as any root's presentation does (the `gallery` root), so it shows the
// generated Kotlin reading what Swift writes; kit-ui draws each. Never
// published.
plugins {
    id("substrate.android.application")
}

apply(plugin = "org.jetbrains.kotlin.plugin.compose")

android {
    namespace = "dev.reamde.substrate.kit.gallery"
    defaultConfig {
        applicationId = "dev.reamde.substrate.gallery"
        versionCode = 1
        versionName = "0.0.0"
    }
    buildFeatures {
        compose = true
    }
    buildTypes {
        getByName("release") {
            // Never published: signed with the debug key so it installs.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

dependencies {
    implementation(project(":kit-ui"))
    implementation(project(":kit-device"))
    implementation(libs.activity.compose)
}
