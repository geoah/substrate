package dev.reamde.substrate.gradle

import com.android.build.api.dsl.ApplicationExtension
import org.gradle.api.JavaVersion
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.kotlin.dsl.configure

/**
 * An Android app: the SDK levels from the catalog, the ABIs Swift was built
 * for, and a release that is minified and shrunk, since that is the build
 * the bridge's runtime checks must pass under (R8 renames what JNI looks up
 * by name unless the kit's consumer rules keep it).
 */
class AndroidApplicationConvention : Plugin<Project> {
    override fun apply(project: Project) = with(project) {
        pluginManager.apply("com.android.application")
        val jvm = JavaVersion.toVersion(libs.pin("jvm"))
        val abis = abis
        extensions.configure<ApplicationExtension> {
            compileSdk = libs.pin("android-compileSdk").toInt()
            buildToolsVersion = libs.pin("android-buildTools")
            ndkVersion = libs.pin("android-ndk")
            defaultConfig {
                minSdk = libs.pin("android-minSdk").toInt()
                targetSdk = libs.pin("android-targetSdk").toInt()
                testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
                ndk { abiFilters += abis }
            }
            buildTypes {
                getByName("release") {
                    isMinifyEnabled = true
                    isShrinkResources = true
                    proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"))
                    if (file("proguard-rules.pro").exists()) proguardFiles("proguard-rules.pro")
                }
            }
            compileOptions {
                sourceCompatibility = jvm
                targetCompatibility = jvm
            }
            packaging {
                // Stored uncompressed and page-aligned, so the loader maps each
                // .so straight from the APK.
                jniLibs.useLegacyPackaging = false
            }
        }
        lockDependencies()
    }
}
