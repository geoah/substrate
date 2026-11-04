package dev.reamde.substrate.gradle

import com.android.build.api.dsl.LibraryExtension
import org.gradle.api.JavaVersion
import org.gradle.api.Plugin
import org.gradle.api.Project
import org.gradle.kotlin.dsl.configure

/** An Android library of the kit's: the SDK levels, the NDK and the JDK from the catalog. */
class AndroidLibraryConvention : Plugin<Project> {
    override fun apply(project: Project) =
        with(project) {
            pluginManager.apply("com.android.library")
            val jvm = JavaVersion.toVersion(libs.pin("jvm"))
            extensions.configure<LibraryExtension> {
                compileSdk = libs.pin("android-compileSdk").toInt()
                buildToolsVersion = libs.pin("android-buildTools")
                ndkVersion = libs.pin("android-ndk")
                defaultConfig {
                    minSdk = libs.pin("android-minSdk").toInt()
                    testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
                    if (file("consumer-rules.pro").exists()) consumerProguardFiles("consumer-rules.pro")
                }
                compileOptions {
                    sourceCompatibility = jvm
                    targetCompatibility = jvm
                }
                testOptions {
                    unitTests.isReturnDefaultValues = false
                }
            }
            lockDependencies()
        }
}
