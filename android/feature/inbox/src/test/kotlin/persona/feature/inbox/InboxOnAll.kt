package persona.feature.inbox

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import persona.core.domain.inbox.InboxBar

/**
 * The Inbox list opened on All, the stacked page with every section, which these tests were written against: since the
 * section bar (#9053) it opens on Needs you.
 */
@Composable
internal fun OnAll(content: @Composable () -> Unit) {
    val landing = remember { InboxLanding().apply { land(InboxBar.Landing(InboxBar.ALL, openCollapsed = false)) } }
    CompositionLocalProvider(LocalInboxLanding provides landing, content = content)
}
