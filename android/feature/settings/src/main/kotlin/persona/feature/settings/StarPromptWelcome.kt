package persona.feature.settings

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import persona.core.designsystem.FeedbackAction
import persona.core.designsystem.FeedbackNotice
import persona.core.designsystem.LocalStarPrompt
import persona.core.designsystem.star

/**
 * The GitHub star toast when first setup finishes (#5761, docs/design/DESIGN_SYSTEM.md "GitHub star prompts"): the
 * checklist's Done marks it owed ([persona.core.designsystem.StarPromptStore.onboarded]) and the app shell shows it
 * through [show], its toast host's, as soon as the shell is up. The checklist page is gone by then (and on a relaunch
 * during setup it is drawn outside the shell, with no toast host at all), so it can't show the toast itself.
 */
@Composable
fun StarPromptWelcome(show: (FeedbackNotice) -> Unit) {
    val prompt = LocalStarPrompt.current ?: return
    val owed by prompt.welcome.collectAsState()
    val context = LocalContext.current
    val message = stringResource(R.string.settings_star_welcome)
    val label = stringResource(R.string.settings_star_on_github)
    LaunchedEffect(owed) {
        if (!owed) return@LaunchedEffect
        show(FeedbackNotice(message, FeedbackAction(label) { prompt.star(context) }))
        prompt.welcomeShown()
    }
}
