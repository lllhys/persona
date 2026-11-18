package persona.feature.inbox

import androidx.compose.runtime.Composable
import persona.core.designsystem.AssistantStackDraftProbe

@Composable
internal fun InboxReplyDraftProbe(page: String?, draft: String, focused: Boolean) {
    AssistantStackDraftProbe(page, draft, focused)
}
