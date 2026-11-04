package dev.reamde.substrate.kit.gallery

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.mutableStateOf
import dev.reamde.substrate.kit.ui.KitTheme

/** The gallery's one activity; `substrate-gallery://fixture/<name>` opens that fixture. */
class MainActivity : ComponentActivity() {
    private val opened = mutableStateOf<String?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        opened.value = fixture(intent)
        setContent {
            KitTheme {
                GalleryApp(GalleryApplication.start, opened.value) { opened.value = it }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        fixture(intent)?.let { opened.value = it }
    }

    private fun fixture(intent: Intent?): String? = intent?.data?.takeIf { it.scheme == "substrate-gallery" && it.host == "fixture" }?.lastPathSegment
}
