package dev.reamde.substrate.tasks.ui

import dev.reamde.substrate.kit.bridge.AccessAction
import dev.reamde.substrate.kit.bridge.SettingsCommand
import dev.reamde.substrate.tasks.bridge.ApprovalPresentation
import dev.reamde.substrate.tasks.bridge.TaskEditor
import dev.reamde.substrate.tasks.bridge.TasksApp

/**
 * Where a screen's commands go: the app's root, an editor by its key, a Settings row, the Access banner. The screens hold no state of the app's and decide
 * nothing; a gallery hands them one that goes nowhere.
 */
interface TasksActions {
    fun send(command: TasksApp.Command)

    fun send(editor: String, command: TaskEditor.Command)

    fun settings(command: SettingsCommand)

    fun perform(action: AccessAction)

    /**
     * The approval the app asks for to change a collection. Android's sign-in (`tasks.connect`) asks for the manifest's scopes alone, so the approval of a kind
     * beside them is not asked here yet, and the question is answered as not given.
     */
    fun approve(approval: ApprovalPresentation) = send(TasksApp.Command.ApprovalAnswered(completed = false))

    object None : TasksActions {
        override fun send(command: TasksApp.Command) = Unit

        override fun send(editor: String, command: TaskEditor.Command) = Unit

        override fun settings(command: SettingsCommand) = Unit

        override fun perform(action: AccessAction) = Unit
    }
}
