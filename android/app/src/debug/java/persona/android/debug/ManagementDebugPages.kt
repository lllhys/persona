package persona.android.debug

import persona.core.navigation.*

/**
 * Captures of pages that take an id (debug builds only): `machine:<id>`, `engine:<id>`, `purpose:<id>`, `extension:<id>`,
 * `extension-ui:<extension>:<ui>`, `market`, `markets`, `market-extension:<market>:<extension>`, `launcher:<machine>:<name>`, `channel:<id>`, `manager:<id>`, `provider:<name>`, and
 * `new-agent` or `new-agent:<machine>:<folder>` (a prefilled form, as the knowledge base opens it).
 */
internal fun managementPage(name: String): ResolvedRoute? {
    val parts = name.split(':', limit = 3)
    val argument = parts.getOrNull(1)?.takeIf { it.isNotBlank() }
    return when (parts[0]) {
        "machine" -> argument?.let { ResolvedRoute(AppTab.Agents, MachineDetailRoute(it)) }
        "engine" -> argument?.let { ResolvedRoute(AppTab.Agents, EngineRoute(it)) }
        "purpose" -> argument?.let { ResolvedRoute(AppTab.Persona, ModelPurposeRoute(it)) }
        "plugin" -> argument?.let { ResolvedRoute(AppTab.Persona, ExtensionRoute(it)) }
        "plugin-ui" -> argument?.let { extension -> parts.getOrNull(2)?.takeIf { it.isNotBlank() }?.let { ResolvedRoute(AppTab.Persona, ExtensionUiRoute(extension, it, it)) } }
        "channel" -> argument?.let { ResolvedRoute(AppTab.Persona, ChannelRoute(it)) }
        "manager" -> argument?.let { ResolvedRoute(AppTab.Persona, ManagerRoute(it)) }
        "provider" -> argument?.let { ResolvedRoute(AppTab.Persona, ModelProviderRoute(it)) }
        "market" -> ResolvedRoute(AppTab.Persona, ExtensionMarketRoute)
        "markets" -> ResolvedRoute(AppTab.Persona, ExtensionMarketsRoute)
        "market-plugin" -> argument?.let { market -> parts.getOrNull(2)?.takeIf { it.isNotBlank() }?.let { ResolvedRoute(AppTab.Persona, ExtensionCatalogRoute(market, it)) } }
        "launcher" -> argument?.let { machine -> parts.getOrNull(2)?.takeIf { it.isNotBlank() }?.let { ResolvedRoute(AppTab.Agents, LauncherRoute(machine, it)) } }
        "new-agent" -> ResolvedRoute(AppTab.Persona, NewAgentRoute(argument, parts.getOrNull(2)?.takeIf { it.isNotBlank() },
            prompt = if (argument == null) null else "这是知识库里这个项目的档案（示例）。\n\n"))  // i18n-ignore: debug preview fixture data
        else -> null
    }
}
