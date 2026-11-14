package dev.reamde.substrate.tasks.ui

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import com.github.takahirom.roborazzi.captureRoboImage
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.ui.KitTheme
import dev.reamde.substrate.tasks.bridge.ListSelection
import dev.reamde.substrate.tasks.bridge.TasksGalleryFixture
import java.io.File
import java.time.ZoneOffset
import kotlinx.serialization.builtins.ListSerializer
import org.junit.Assert.assertNotNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.ParameterizedRobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Tasks' fixtures, as Swift wrote them (`fixtures/tasks.json`). */
internal object TasksFixtures {
    val all: List<TasksGalleryFixture> by lazy {
        BridgeJson.decodeFromString(ListSerializer(TasksGalleryFixture.serializer()), File(System.getProperty("substrate.tasks.fixtures")).readText())
    }

    val folder: File
        get() = File(System.getProperty("substrate.gallery") ?: "build/gallery/android/tasks").apply { mkdirs() }

    /**
     * A fixture's screen, drawn by the composable the app draws it with, as the Apple gallery draws its views; nil for the delete-list question, a system
     * dialog neither gallery draws, whose words TasksKit's tests hold.
     */
    fun screen(fixture: TasksGalleryFixture): (@Composable () -> Unit)? {
        val app = fixture.app
        val none = TasksActions.None
        return when (fixture.screen) {
            TasksGalleryFixture.Screen.sidebar -> {
                { SidebarScreen(app.sidebar, app, null, none, {}, showsNeedsYou = false, selected = app.sidebar.selected) }
            }
            TasksGalleryFixture.Screen.list ->
                app.list?.let { list ->
                    { TaskListScreen(list, app, null, none, {}, onBack = if (list.selection is ListSelection.Group) ({}) else null) }
                }
            TasksGalleryFixture.Screen.detail -> app.detail?.let { detail -> { TaskDetailScreen(detail, none, onBack = {}) } }
            TasksGalleryFixture.Screen.editor -> app.editors[fixture.editor]?.let { editor -> { TaskEditorForm(editor, {}, zone = ZoneOffset.UTC) } }
            TasksGalleryFixture.Screen.confirmation -> null
            TasksGalleryFixture.Screen.settings -> fixture.settings?.let { settings -> { SettingsPane(settings, none) } }
            TasksGalleryFixture.Screen.queue -> app.queue?.let { queue -> { SuggestionsScreen(queue, app, none, onBack = {}) } }
            TasksGalleryFixture.Screen.suggestion -> app.suggestion?.let { suggestion -> { SuggestionScreen(suggestion, none, onBack = {}) } }
            TasksGalleryFixture.Screen.choice ->
                app.choice?.let { choice ->
                    {
                        app.queue?.let { SuggestionsScreen(it, app, none, onBack = {}) }
                        ChoiceDialog(choice, none)
                    }
                }
            TasksGalleryFixture.Screen.collections -> {
                { CollectionsScreen(app.collections, none, onBack = {}) }
            }
            TasksGalleryFixture.Screen.collection -> app.collection?.let { collection -> { CollectionScreen(collection, none, onBack = {}) } }
            TasksGalleryFixture.Screen.approval ->
                app.approval?.let { approval ->
                    {
                        CollectionsScreen(app.collections, none, onBack = {})
                        ApprovalDialog(approval, none)
                    }
                }
        }
    }
}

/**
 * Substrate Tasks' screens drawn from TasksKit's fixtures by the composables the app draws them with, on the JVM, at the size the Apple gallery draws an iPhone
 * in, light: each fixture but the system dialog must have its screen, and each is written as `<fixture>.png` (the Apple gallery's names) where
 * `-Psubstrate.gallery` says.
 */
@RunWith(ParameterizedRobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(qualifiers = "w393dp-h852dp-xhdpi")
class TasksGalleryTest(private val name: String) {
    @get:Rule val compose = createComposeRule()

    @Test
    fun theFixtureDraws() {
        val fixture = TasksFixtures.all.first { it.name == name }
        if (fixture.screen == TasksGalleryFixture.Screen.confirmation) return
        val screen = TasksFixtures.screen(fixture)
        assertNotNull("$name has no screen", screen)
        compose.mainClock.autoAdvance = false
        compose.setContent { KitTheme(dark = false) { screen!!() } }
        compose.mainClock.advanceTimeBy(400)
        compose.onRoot().captureRoboImage(File(TasksFixtures.folder, "$name.png").path)
    }

    companion object {
        @JvmStatic @ParameterizedRobolectricTestRunner.Parameters(name = "{0}") fun fixtures(): List<Array<Any>> = TasksFixtures.all.map { arrayOf(it.name) }
    }
}

/** The wide layout, as the Apple gallery draws the Mac's split view: the App menu as a drawer, the lists, a list and the open task side by side. */
@RunWith(ParameterizedRobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(qualifiers = "w1280dp-h800dp-mdpi")
class TasksWideGalleryTest(private val name: String) {
    @get:Rule val compose = createComposeRule()

    @Test
    fun theWideLayoutDraws() {
        val fixture = TasksFixtures.all.first { it.name == name }
        compose.mainClock.autoAdvance = false
        compose.setContent { KitTheme(dark = false) { TasksAppView(fixture.app, null, null, TasksActions.None) } }
        compose.mainClock.advanceTimeBy(400)
        compose.onRoot().captureRoboImage(File(TasksFixtures.folder, "$name-wide.png").path)
    }

    companion object {
        @JvmStatic
        @ParameterizedRobolectricTestRunner.Parameters(name = "{0}")
        fun fixtures(): List<Array<Any>> = listOf("tasks-detail", "tasks-needs-you", "tasks-suggestion", "tasks-group").map { arrayOf<Any>(it) }
    }
}
