// The Android build of every app (docs/plans/app-kit.md, §5.3 and §6): one
// Gradle root here, whose modules live with what they build. The kit's are
// under apps/kit/android/, an app's under apps/<app>/android/.
pluginManagement {
    includeBuild("build-logic")
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "substrate-apps"

fun module(name: String, dir: String) {
    include(name)
    project(name).projectDir = file(dir)
}

module(":kit-core", "../kit/android/kit-core")
module(":kit-device", "../kit/android/kit-device")
module(":bridge-check", "../kit/android/bridge-check")
