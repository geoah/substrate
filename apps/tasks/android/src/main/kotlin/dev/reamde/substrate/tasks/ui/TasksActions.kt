package dev.reamde.substrate.tasks.ui

import dev.reamde.substrate.kit.bridge.AccessAction
import dev.reamde.substrate.kit.bridge.SettingsCommand
import dev.reamde.substrate.tasks.bridge.ApprovalLine
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

    /** The approval the app asks for to change a collection or read a group kind, through the sign-in with its scopes beside the manifest's. */
    fun approve(approval: ApprovalPresentation) = send(TasksApp.Command.ApprovalAnswered(completed = false))

    /** A page's approval line: the owner goes to the approval page at once. */
    fun request(line: ApprovalLine) = send(TasksApp.Command.ApprovalAnswered(completed = false))

    object None : TasksActions {
        override fun send(command: TasksApp.Command) = Unit

        override fun send(editor: String, command: TaskEditor.Command) = Unit

        override fun settings(command: SettingsCommand) = Unit

        override fun perform(action: AccessAction) = Unit
    }
}
