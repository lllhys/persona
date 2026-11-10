package persona.feature.settings

import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.filter
import persona.core.contracts.api.ExtensionManagement
import persona.core.contracts.api.SUBSCRIPTIONS_UI
import persona.core.contracts.api.SubscriptionSummaryLine
import persona.core.contracts.api.subscriptionCounts
import persona.core.designsystem.GroupPosition
import persona.core.designsystem.JGroupItem
import persona.core.designsystem.JRow
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.ExtensionUiRoute

/**
 * How many of the connectors [pick] chooses are on and failing (source subscriptions, #2041): read once the hub says
 * subscriptions deliver, and again (at most once a second) when one changes; null while the entry row stays hidden.
 */
@Composable
internal fun rememberSubscriptionCounts(extensions: ExtensionManagement?, pick: (SubscriptionSummaryLine) -> Boolean): Pair<Int, Int>? {
    if (extensions == null) return null
    val available by extensions.subscriptionsAvailable.collectAsState(initial = false)
    var lines by remember(extensions) { mutableStateOf<List<SubscriptionSummaryLine>?>(null) }
    LaunchedEffect(extensions, available) {
        if (!available) {
            lines = null
            return@LaunchedEffect
        }
        suspend fun load() {
            try {
                lines = extensions.subscriptionSummary()
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // an entry row is a convenience: it keeps what it had, or stays hidden
            }
        }
        load()
        extensions.uiEvents.filter { it.extension == SUBSCRIPTIONS_UI }.conflate().collect {
            delay(1_000)
            load()
        }
    }
    return lines?.subscriptionCounts(pick)
}

/** The pages for one extension's or one connector's subscriptions (their first state), or all of them. */
fun subscriptionsRoute(title: String, extension: String? = null, connector: String? = null) = ExtensionUiRoute(
    SUBSCRIPTIONS_UI, "pages", title,
    state = when {
        extension != null -> """{"ext":${kotlinx.serialization.json.JsonPrimitive(extension)}}"""
        connector != null -> """{"c":${kotlinx.serialization.json.JsonPrimitive(connector)}}"""
        else -> null
    },
)

/** The entry row: Subscriptions, with how many are on and failing; nothing when [counts] is null. */
internal fun LazyListScope.subscriptionsEntry(counts: Pair<Int, Int>?, onOpen: () -> Unit) {
    val (on, failing) = counts ?: return
    item("subscriptions") {
        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.m)) {
            val subtitle = when {
                failing > 0 -> stringResource(R.string.settings_subscriptions_counts_failing, on, failing)
                on > 0 -> stringResource(R.string.settings_subscriptions_counts_on, on)
                else -> stringResource(R.string.settings_subscriptions_counts_none)
            }
            JRow(stringResource(R.string.settings_subscriptions_row), subtitle = subtitle, leadingIcon = PhIcons.ArrowsClockwise, onClick = onOpen)
        }
    }
}
