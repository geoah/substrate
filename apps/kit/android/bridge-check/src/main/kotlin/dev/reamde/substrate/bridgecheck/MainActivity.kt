package dev.reamde.substrate.bridgecheck

import android.app.Activity
import android.os.Bundle
import android.util.Log
import android.widget.ScrollView
import android.widget.TextView
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Runs BridgeChecks once the first frame is up and shows each result; every
 * line also goes to the log under `bridge-check`, which `android:smoke`
 * reads. `--ez checks false` launches without running them, for timing a
 * cold start alone.
 */
class MainActivity : Activity() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val text = TextView(this).apply { setPadding(32, 32, 32, 32) }
        setContentView(ScrollView(this).apply { addView(text) })
        text.text = "start: ${CheckApplication.start} in ${"%.1f".format(CheckApplication.startMillis)} ms\n"
        if (!intent.getBooleanExtra("checks", true)) return
        scope.launch {
            val results = withContext(Dispatchers.Default) {
                BridgeChecks(this@MainActivity) { line ->
                    Log.i(TAG, line)
                    scope.launch { text.append(line + "\n") }
                }.runAll()
            }
            val failed = results.count { !it.passed }
            val summary = if (failed == 0) "PASS all ${results.size}" else "FAIL $failed of ${results.size}"
            Log.i(TAG, "done: $summary")
            text.append("done: $summary\n")
        }
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private companion object {
        const val TAG = "bridge-check"
    }
}
