package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.FixturePresentation
import dev.reamde.substrate.kit.bridge.ScreenFixture
import dev.reamde.substrate.kit.bridge.ScreenKind

/** Which shared screen a presentation is of. */
public val FixturePresentation.screen: ScreenKind
    get() = when (this) {
        is FixturePresentation.Connect -> ScreenKind.connect
        is FixturePresentation.ServerTooOld -> ScreenKind.serverTooOld
        is FixturePresentation.AccessBanner -> ScreenKind.accessBanner
        is FixturePresentation.AccessEnded -> ScreenKind.accessEnded
        is FixturePresentation.Review -> ScreenKind.review
        is FixturePresentation.Repository -> ScreenKind.repository
        is FixturePresentation.Status -> ScreenKind.status
        is FixturePresentation.Settings -> ScreenKind.settings
        is FixturePresentation.Problem -> ScreenKind.problem
        is FixturePresentation.About -> ScreenKind.about
        is FixturePresentation.Menu -> ScreenKind.menu
    }

/**
 * Every shared screen's composable, by screen: what the gallery draws each
 * fixture with (§7.4), as SubstrateUI's `Gallery` does. A screen added to
 * `ScreenKind` without one here fails kit-ui's gallery test, so a
 * presentation never ships without its Android view.
 */
public object KitGallery {
    /** The fixtures' app, as `ScreenFixtures` names it. */
    private const val APP = "Substrate Tasks"

    public val renderers: Map<ScreenKind, @Composable (FixturePresentation) -> Unit> = mapOf(
        ScreenKind.connect to { presentation ->
            (presentation as? FixturePresentation.Connect)?.let {
                ConnectScreen(
                    it.value,
                    send = {},
                    header = { ConnectHeader(APP, tagline = "Your tasks, in your substrate.") },
                    passwordDoor = { PasswordDoorStandIn() },
                )
            }
        },
        ScreenKind.serverTooOld to { presentation ->
            (presentation as? FixturePresentation.ServerTooOld)?.let {
                Scrolling(padding = KitSpacing.screen) { ServerTooOldScreen(it.value, onTryAgain = {}, onChangeServer = {}) }
            }
        },
        ScreenKind.accessBanner to { presentation ->
            (presentation as? FixturePresentation.AccessBanner)?.let { Scrolling(padding = KitSpacing.section) { AccessBannerView(it.value, perform = {}) } }
        },
        ScreenKind.accessEnded to { presentation ->
            (presentation as? FixturePresentation.AccessEnded)?.let {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { AccessEndedScreen(it.value, onSignInAgain = {}) }
            }
        },
        ScreenKind.review to { presentation ->
            (presentation as? FixturePresentation.Review)?.let { ReviewScreen(it.value, onContinue = {}, onCancel = {}) }
        },
        ScreenKind.repository to { presentation ->
            (presentation as? FixturePresentation.Repository)?.let { Form { RepositoryScreen(it.value, onSignOut = {}, onChangeAccess = {}) } }
        },
        ScreenKind.status to { presentation ->
            (presentation as? FixturePresentation.Status)?.let { Form { StatusScreen(it.value, perform = {}) } }
        },
        ScreenKind.settings to { presentation ->
            (presentation as? FixturePresentation.Settings)?.let { SettingsScreen(it.value, send = {}) }
        },
        ScreenKind.problem to { presentation ->
            (presentation as? FixturePresentation.Problem)?.let { problem ->
                Scrolling(padding = KitSpacing.section) { KitCard(tint = problem.value.tone.tint) { ProblemView(problem.value) } }
            }
        },
        ScreenKind.about to { presentation ->
            (presentation as? FixturePresentation.About)?.let { Form { AboutScreen(it.value) } }
        },
        ScreenKind.menu to { presentation ->
            (presentation as? FixturePresentation.Menu)?.let {
                AppMenu(it.value, select = {}, title = APP, layout = AppMenuLayout.Bar) { section -> DestinationStandIn(section.title) }
            }
        },
    )

    /** The fixture drawn by its screen's composable; nothing where there is none. */
    @Composable
    public fun Render(fixture: ScreenFixture) {
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
            renderers[fixture.presentation.screen]?.invoke(fixture.presentation)
        }
    }

    @Composable
    private fun Scrolling(padding: androidx.compose.ui.unit.Dp, content: @Composable () -> Unit) {
        Box(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(padding), contentAlignment = Alignment.TopCenter) { content() }
    }

    /** A settings screen's sections, scrolled, as a grouped form holds them. */
    @Composable
    private fun Form(content: @Composable () -> Unit) {
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(vertical = KitSpacing.medium)) { content() }
    }

    /** Where an app's own password form stands: the kit draws the door, the app the form. */
    @Composable
    private fun PasswordDoorStandIn() {
        KitCard {
            Text("The app's own password form", style = KitText.heading.style)
            Surface(shape = RoundedCornerShape(KitRadius.control), color = MaterialTheme.colorScheme.surfaceContainerHighest,
                    modifier = Modifier.fillMaxWidth().heightIn(min = 44.dp)) {
                Text("Password", color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(KitSpacing.row))
            }
        }
    }

    /** Where an app's own screen stands under its menu. */
    @Composable
    private fun DestinationStandIn(title: String) {
        Column(Modifier.fillMaxSize().padding(KitSpacing.screen), verticalArrangement = Arrangement.spacedBy(KitSpacing.content)) {
            Text(title, style = KitText.appTitle.style)
            Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                Text("The app's $title screen", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
