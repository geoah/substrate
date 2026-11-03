package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.AccessBanner
import dev.reamde.substrate.kit.bridge.NoticeLine
import dev.reamde.substrate.kit.ui.AccessBannerView
import dev.reamde.substrate.kit.ui.KitCard
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.icon
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.kit.ui.tint
import dev.reamde.substrate.tasks.bridge.TasksApp

/**
 * What stands in the way above a column: the link's Access banner and the
 * app's notices (the tasks not being read), each line under its mark in its
 * tone. A notice opens Settings, where its Retry and Discard are.
 */
@Composable
internal fun Notices(banner: AccessBanner?, lines: List<NoticeLine>, actions: TasksActions) {
    if (banner == null && lines.isEmpty()) return
    Column(Modifier.fillMaxWidth().padding(horizontal = KitSpacing.content, vertical = KitSpacing.medium),
           verticalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
        banner?.let { AccessBannerView(it, actions::perform) }
        if (lines.isNotEmpty()) {
            KitCard(Modifier.clickable { actions.send(TasksApp.Command.Select(MENU_SETTINGS)) }, spacing = KitSpacing.small) {
                for (line in lines) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
                        Icon(line.symbol.icon, contentDescription = null, tint = line.tone.tint, modifier = Modifier.size(18.dp))
                        Text(line.text, style = KitText.secondary.style)
                    }
                }
            }
        }
    }
}
