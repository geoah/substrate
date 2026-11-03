package dev.reamde.substrate.tasks.ui

import dev.reamde.substrate.kit.bridge.AccessAction
import dev.reamde.substrate.kit.bridge.SettingsCommand
import dev.reamde.substrate.tasks.bridge.TaskEditor
import dev.reamde.substrate.tasks.bridge.TasksApp

/**
 * Where a screen's commands go: the app's root, an editor by its key, a
 * Settings row, the Access banner. The screens hold no state of the app's
 * and decide nothing; a gallery hands them one that goes nowhere.
 */
interface TasksActions {
    fun send(command: TasksApp.Command)

    fun send(editor: String, command: TaskEditor.Command)

    fun settings(command: SettingsCommand)

    fun perform(action: AccessAction)

    object None : TasksActions {
        override fun send(command: TasksApp.Command) = Unit

        override fun send(editor: String, command: TaskEditor.Command) = Unit

        override fun settings(command: SettingsCommand) = Unit

        override fun perform(action: AccessAction) = Unit
    }
}
