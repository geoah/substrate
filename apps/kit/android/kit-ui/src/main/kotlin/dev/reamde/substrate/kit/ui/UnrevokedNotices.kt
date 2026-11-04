package dev.reamde.substrate.kit.ui

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import dev.reamde.substrate.kit.bridge.ProblemPresentation
import dev.reamde.substrate.kit.bridge.ScreenTone

/**
 * The tokens a device stopped using that their servers have not revoked yet, as `UnrevokedPresenter` words them: each in a card of its own on a screen, or as a
 * row of a section (`asRows`).
 */
@Composable
public fun UnrevokedNotices(notices: List<ProblemPresentation>, modifier: Modifier = Modifier, asRows: Boolean = false) {
    notices.forEachIndexed { index, notice ->
        if (asRows) {
            if (index > 0) KitRowDivider()
            ProblemBox(notice, modifier.kitRowPadding())
        } else {
            KitCard(modifier, tint = ScreenTone.caution.tint) { ProblemBox(notice) }
        }
    }
}
