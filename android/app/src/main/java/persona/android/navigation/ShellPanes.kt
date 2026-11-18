package persona.android.navigation

import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.remember
import androidx.navigation3.runtime.NavEntry
import androidx.navigation3.runtime.NavKey
import persona.core.designsystem.SplitDetailContext
import persona.core.navigation.*
import persona.feature.inbox.ConnectorThreadRoute
import persona.feature.inbox.InboxEventRoute
import persona.feature.inbox.InboxItemDetailRoute
import persona.feature.inbox.LarkMomentRoute
import persona.feature.inbox.LarkRoute
import persona.feature.inbox.MailRoute
import persona.platform.phonetools.PhoneToolsRoute

/**
 * Destinations that can open beside a list when the window has room for two panes: the Agents list and the secondary
 * Inbox and Settings lists. The shell gives these the list-detail scene's detail role and nothing else does.
 */
internal fun isDetailDestination(key: NavKey): Boolean = when (key) {
    is ChatRoute, is InteractionRoute, is TerminalRoute, is LarkRoute, is LarkMomentRoute, is MailRoute, is InboxItemRoute, is LarkChatRoute,
    is InboxItemUiRoute, is InboxItemDetailRoute, is InboxEventRoute,
    is SettingsPageRoute, PairingRoute, PhoneToolsRoute, MachinesRoute, is MachineDetailRoute, is AddComputerRoute, TerminalPanesRoute,
    is EngineRoute, is ModelPurposeRoute, is ModelProviderRoute, AssistantSettingsRoute, is ExtensionRoute, is ExtensionUiRoute,
    ExtensionMarketRoute, is ExtensionCatalogRoute, ExtensionMarketsRoute, is LauncherRoute, is ChannelRoute, is ManagerRoute, is ConnectorRoute -> true
    // A space's page and a computer's projects open beside the Agents list too; a conversation opened from them goes on top.
    is SpaceRoute, is MachineSpacesRoute -> true
    else -> false
}

/** The Inbox list and every detail that replaces it in the Assistant's single right-hand stack. */
internal fun isInboxDestination(key: NavKey): Boolean = when (key) {
    InboxRoute, is LarkRoute, is LarkMomentRoute, is MailRoute, is InboxItemRoute, is LarkChatRoute,
    is InboxItemUiRoute, is InboxItemDetailRoute, is InboxEventRoute, is ConnectorThreadRoute -> true
    else -> false
}

/** Secondary Settings/Inbox lists never inherit the primary Agents page's collapse policy. */
internal fun canCollapseList(stack: List<NavKey>): Boolean =
    stack.lastOrNull { it == AgentsRoute || it == InboxRoute || it == SettingsRoute } == AgentsRoute

internal data class AgentsTabSplit(
    val stack: List<NavKey>,
    val contentKeys: List<Any>,
    val listHidden: Boolean,
    val onShowList: () -> Unit,
)

internal val LocalAgentsTabSplit = compositionLocalOf<AgentsTabSplit?> { null }

/**
 * Split-pane context for one entry at [entryIndex] in the Agents tab's two-pane list-detail layout (matching iOS
 * `SplitDetail`):
 * - The list entry itself (`entryIndex <= 0`) gets null so a Back button inside it is never hidden by accident.
 * - First detail page directly above `AgentsRoute` (`entryIndex == 1`): `isFirstPage = true` (stays true while deeper
 *   pages push above it, so its leading button does not jump during transitions).
 * - Deeper pushed pages (`entryIndex > 1`): `isFirstPage = false` so Back stays as usual.
 * - Compact/overlaid layouts, secondary lists on the Agents stack, or other tabs: null.
 */
internal fun agentsSplitDetailForEntry(
    tab: AppTab,
    stack: List<NavKey>,
    entryIndex: Int,
    twoPanes: Boolean,
    listHidden: Boolean,
    onShowList: () -> Unit,
): SplitDetailContext? {
    if (!twoPanes || tab != AppTab.Agents || entryIndex <= 0 || entryIndex >= stack.size) return null
    val prefix = stack.subList(0, entryIndex + 1)
    if (!canCollapseList(prefix) || !prefix.drop(1).all(::isDetailDestination)) return null
    return SplitDetailContext(
        isFirstPage = entryIndex == 1,
        listHidden = listHidden,
        onShowList = if (listHidden) onShowList else null,
    )
}

@Composable
internal fun rememberAgentsTabSplit(
    tab: AppTab,
    stack: List<NavKey>,
    twoPanes: Boolean,
    listHidden: Boolean,
    onShowList: () -> Unit,
): AgentsTabSplit? {
    if (!twoPanes || tab != AppTab.Agents) return null
    val snapshot = stack.toList()
    val contentKeys = remember(snapshot) { snapshot.map { NavEntry(it) {}.contentKey } }
    return AgentsTabSplit(snapshot, contentKeys, listHidden, onShowList)
}

/**
 * Resolves per-entry [SplitDetailContext] inside the chrome decorator, retaining the entry's last resolved state
 * while it slides away during a pop transition after leaving [AgentsTabSplit.stack].
 */
@Composable
internal fun rememberEntrySplitDetail(
    tabSplit: AgentsTabSplit?,
    entryContentKey: Any,
): SplitDetailContext? {
    val kept = remember(entryContentKey) { arrayOfNulls<SplitDetailContext?>(1) }
    if (tabSplit == null) {
        kept[0] = null
        return null
    }
    val entryIndex = tabSplit.contentKeys.indexOf(entryContentKey)
    if (entryIndex >= 0) {
        return agentsSplitDetailForEntry(
            tab = AppTab.Agents,
            stack = tabSplit.stack,
            entryIndex = entryIndex,
            twoPanes = true,
            listHidden = tabSplit.listHidden,
            onShowList = tabSplit.onShowList,
        ).also { kept[0] = it }
    }
    return kept[0]?.let { previous ->
        previous.copy(
            listHidden = tabSplit.listHidden,
            onShowList = if (tabSplit.listHidden) tabSplit.onShowList else null,
        )
    }
}

/**
 * The detail a tab's list marks as open beside it (`LocalDetailKey`): the top of the tab's stack when that is a
 * detail, however it got there (a tap in the list, a deep link, a notification, a debug launch, back). Pages that also
 * open under another key come back as the settings page whose row the list shows.
 *
 * Decided from the keys, not from the entries' scene metadata: `ListDetailSceneStrategy.detailPane()` builds a new
 * `DetailMetadata` on every call and it has no equality, so comparing metadata never matched and no list row was
 * ever marked (2026-11-02).
 */
internal fun detailOf(stack: List<NavKey>): NavKey? = when (val top = stack.lastOrNull()) {
    null -> null
    PairingRoute -> SettingsPageRoute(SettingsPage.Pairing)
    PhoneToolsRoute -> SettingsPageRoute(SettingsPage.PhoneData)
    else -> top.takeIf(::isDetailDestination)
}

/** What the home top bar's leading slot shows and does on the primary tabs. */
internal enum class HomeLeadingAction {
    /** Opens the shared navigation drawer (compact windows and the Agents tab). */
    OpenDrawer,
    /** Toggles the floating overlay sidebar over the page (upright open foldable). */
    ToggleOverlay,
    /** Reopens the folded sidebar column beside the page (wide Assistant root). */
    ShowColumn,
    /** Keeps the button's room empty while the sidebar column is out beside the page. */
    HiddenSpacer,
}

internal fun resolveHomeLeadingAction(
    assistantRoot: Boolean,
    regular: Boolean,
    columnsOver: Boolean,
    sidebarColumnShown: Boolean,
): HomeLeadingAction = when {
    !assistantRoot || !regular -> HomeLeadingAction.OpenDrawer
    columnsOver -> HomeLeadingAction.ToggleOverlay
    sidebarColumnShown -> HomeLeadingAction.HiddenSpacer
    else -> HomeLeadingAction.ShowColumn
}

/** The column header folds the column only where the sidebar is a column beside the page: never over it or in a drawer. */
internal fun showsSidebarColumnFoldButton(regular: Boolean, columnsOver: Boolean): Boolean = regular && !columnsOver
