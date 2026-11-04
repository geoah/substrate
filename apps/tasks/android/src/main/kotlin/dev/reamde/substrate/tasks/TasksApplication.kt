package dev.reamde.substrate.tasks

import android.app.Application
import android.util.Log
import dev.reamde.substrate.kit.Bridge
import dev.reamde.substrate.kit.device.KitDevice

/**
 * Starts the bridge where every app does, Application.onCreate on the main thread, over this app's own library, and makes the shell once per process, so an
 * activity made again finds the handles it left.
 */
class TasksApplication : Application() {
    lateinit var shell: TasksShell
        private set

    override fun onCreate() {
        super.onCreate()
        val start = Bridge.start(this, KitDevice.services(this, notificationChannel = "Reminders"), library = "SubstrateTasks")
        Log.i("tasks", "start: $start")
        shell = TasksShell(this, start)
    }
}
