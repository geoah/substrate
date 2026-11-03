package dev.reamde.substrate.kit

import android.content.Context
import java.io.File

/**
 * The Swift packages' resource bundles as a folder `Bundle(path:)` reads.
 * SwiftPM's `Bundle.module` looks beside `Bundle.main`, which on Android is
 * `/system/bin`, so the APK carries the bundles as assets (`swift-resources/`,
 * put there by the Swift build) and they are copied out once per install.
 */
internal object SwiftResources {
    private const val ASSETS = "swift-resources"

    fun folder(context: Context): File {
        val root = File(context.noBackupFilesDir, ASSETS)
        val stamp = File(root, ".installed")
        val installed = context.packageManager.getPackageInfo(context.packageName, 0).lastUpdateTime.toString()
        if (stamp.isFile && stamp.readText() == installed) return root
        root.deleteRecursively()
        copy(context, ASSETS, root)
        root.mkdirs()
        stamp.writeText(installed)
        return root
    }

    private fun copy(context: Context, asset: String, target: File) {
        val children = context.assets.list(asset).orEmpty()
        if (children.isEmpty()) {
            target.parentFile?.mkdirs()
            context.assets.open(asset).use { input -> target.outputStream().use { input.copyTo(it) } }
            return
        }
        target.mkdirs()
        for (child in children) copy(context, "$asset/$child", File(target, child))
    }
}
