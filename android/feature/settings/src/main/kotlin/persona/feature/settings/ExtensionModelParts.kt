package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.Text
import android.content.res.Resources
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import java.time.Instant
import persona.core.contracts.api.ExtensionDependency
import persona.core.contracts.api.ExtensionDetail
import persona.core.contracts.api.ExtensionPackMember
import persona.core.contracts.api.ExtensionPreview
import persona.core.contracts.api.ExtensionSandbox
import persona.core.designsystem.*

internal object ExtensionModelText {
    fun dependency(res: Resources, d: ExtensionDependency, installed: Boolean = false): String = listOfNotNull(
        d.name?.ifBlank { null } ?: d.id,
        d.version?.ifBlank { null },
        when (d.state) {
            "ok" -> res.getString(R.string.settings_ext_dep_ok)
            "missing" -> res.getString(R.string.settings_ext_dep_missing)
            "disabled" -> res.getString(R.string.settings_ext_dep_disabled)
            "not_ready" -> res.getString(R.string.settings_ext_dep_not_ready)
            "old" -> res.getString(R.string.settings_ext_dep_old, d.installed.orEmpty())
            else -> res.getString(if (installed) R.string.settings_ext_dep_unknown_installed else R.string.settings_ext_dep_unknown)
        },
    ).joinToString(" · ")

    fun blockedPreview(res: Resources, preview: ExtensionPreview): String = res.getString(if (
        preview.depends.any { it.state == "not_ready" } || preview.bundle.any { member -> member.depends.any { it.state == "not_ready" } }
    ) R.string.settings_ext_cannot_confirm_not_ready else R.string.settings_ext_cannot_confirm)

    fun sandbox(res: Resources, s: ExtensionSandbox): String = when {
        s.active -> res.getString(R.string.settings_ext_sandbox_active)
        s.enabled -> res.getString(R.string.settings_ext_sandbox_inactive)
        else -> res.getString(R.string.settings_ext_sandbox_off)
    }
}

/** A pack member by its [name] when known, the id beside its version and market (only the id when it isn't named, #4122). */
internal fun packMemberLine(member: ExtensionPackMember, name: String?): Pair<String, String?> =
    (name ?: member.id) to listOfNotNull(member.id.takeIf { name != null }, member.version, member.market).joinToString(" · ").ifBlank { null }

/** [names]: the pack's members by their installed names (#4122); one not installed keeps its id. */
internal fun LazyListScope.installedExtensionModel(detail: ExtensionDetail, names: Map<String, String> = emptyMap()) {
    if (detail.depends.isNotEmpty()) {
        item("dependencies-title") { JSectionHeader(stringResource(R.string.settings_ext_depends)) }
        detail.depends.forEachIndexed { index, dependency ->
            item("dependency-$index") {
                JGroupItem(GroupPosition.of(index, detail.depends.size)) {
                    JInlineStatus(ExtensionModelText.dependency(LocalResources.current, dependency, installed = true),
                        tone = if (dependency.state == "ok") InlineTone.Done else InlineTone.Caution)
                }
            }
        }
    }
    if (detail.extensionPack.isNotEmpty()) {
        item("pack-members-title") { JSectionHeader(stringResource(R.string.settings_ext_pack_members)) }
        detail.extensionPack.forEachIndexed { index, member ->
            item("pack-member-$index") {
                JGroupItem(GroupPosition.of(index, detail.extensionPack.size)) {
                    val (title, subtitle) = packMemberLine(member, names[member.id])
                    JRow(title, subtitle = subtitle, subtitleLines = Int.MAX_VALUE, chevron = false)
                }
            }
        }
    }
}

internal fun LazyListScope.sandbox(s: ExtensionSandbox?, name: String, onAllow: (() -> Unit)? = null) {
    s ?: return
    item("sandbox-title") { JSectionHeader(stringResource(R.string.settings_ext_sandbox_title)) }
    item("sandbox-state") {
        JGroupItem(GroupPosition.Single) {
            JRow(ExtensionModelText.sandbox(LocalResources.current, s), subtitle = s.why?.ifBlank { null }, subtitleLines = 4)
        }
    }
    s.egress?.let { e ->
        item("sandbox-egress") {
            JGroupItem(GroupPosition.Single) {
                val allowed = pluralStringResource(R.plurals.settings_ext_sandbox_allowed, e.allowed.quantity(), e.allowed)
                val blocked = pluralStringResource(R.plurals.settings_ext_sandbox_blocked, e.denied.quantity(), e.denied)
                val separator = stringResource(R.string.settings_ext_dot_separator)
                val palette = PersonaTheme.palette
                JRow(stringResource(R.string.settings_ext_sandbox_network), trailing = {
                    Text(buildAnnotatedString {
                        withStyle(SpanStyle(color = palette.positive)) { append(allowed) }
                        append(separator)
                        withStyle(SpanStyle(color = if (e.denied > 0) palette.danger.text else palette.ink3)) { append(blocked) }
                    }, style = PersonaTheme.type.callout, maxLines = 1, overflow = TextOverflow.Ellipsis)
                })
            }
        }
        e.lastDenied?.ifBlank { null }?.let { host ->
            item("sandbox-denied") {
                val res = LocalResources.current
                val ago = e.deniedAt?.let { at -> runCatching { agoText(res, Instant.parse(at)) }.getOrNull() }
                JNotice(
                    stringResource(R.string.settings_ext_sandbox_blocked_sentence, name, host) + ago?.let { "\n$it" }.orEmpty(),
                    Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou,
                    actionLabel = if (onAllow == null) null else stringResource(R.string.settings_ext_sandbox_go_admin),
                    onAction = onAllow,
                )
            }
        }
    }
    item("sandbox-note") {
        SettingsCaption(stringResource(if (s.egress == null) R.string.settings_ext_sandbox_note else R.string.settings_ext_sandbox_footer),
            Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
    }
}

@Composable
internal fun BundlePreview(preview: ExtensionPreview, installedNames: Map<String, String> = emptyMap()) {
    val res = LocalResources.current
    // A pack's members named (#4122): those installed with it by their manifests, those already here by the installed list.
    val names = installedNames + preview.bundle.mapNotNull { b -> b.manifest?.let { m -> m.id to (m.name?.ifBlank { null } ?: m.id) } }
    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        if (preview.depends.isNotEmpty()) {
            Text(stringResource(R.string.settings_ext_depends), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            preview.depends.forEach { JInlineStatus(ExtensionModelText.dependency(res, it),
                tone = if (it.state == "ok") InlineTone.Done else InlineTone.Caution) }
        }
        preview.manifest?.extensionPack?.takeIf { it.isNotEmpty() }?.let { members ->
            Text(stringResource(R.string.settings_ext_pack_members), style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            members.forEach { member -> val (title, subtitle) = packMemberLine(member, names[member.id]); JRow(title, subtitle = subtitle) }
            SettingsCaption(stringResource(R.string.settings_ext_pack_note))
        }
        if (preview.bundle.isNotEmpty()) {
            Text(pluralStringResource(R.plurals.settings_ext_bundle_title, preview.bundle.size, preview.bundle.size), style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink)
            preview.bundle.forEach { member ->
                Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                    JRow(member.manifest?.name?.ifBlank { null } ?: member.manifest?.id.orEmpty(),
                        subtitle = member.manifest?.version, value = stringResource(if (member.upgrade) R.string.settings_ext_upgrade else R.string.settings_ext_install))
                    PermissionList(stringResource(R.string.settings_ext_it_can), member.permissions)
                    if (member.permissions.isEmpty()) SettingsCaption(stringResource(R.string.settings_ext_no_permissions))
                    if (!member.addedPermissions.isNullOrEmpty()) PermissionList(stringResource(R.string.settings_ext_added_permissions), member.addedPermissions.orEmpty())
                    member.depends.forEach { SettingsCaption(ExtensionModelText.dependency(res, it)) }
                    member.warnings.orEmpty().forEach { JInlineStatus(it, tone = InlineTone.Caution) }
                }
            }
        }
        preview.unresolved.forEach { JInlineStatus(stringResource(R.string.settings_ext_unresolved, it), tone = InlineTone.Failed) }
        if (!preview.dependenciesReady) JInlineStatus(ExtensionModelText.blockedPreview(res, preview), tone = InlineTone.Caution)
    }
}

/** A count for picking a plural form. */
internal fun Long.quantity(): Int = coerceIn(0L, Int.MAX_VALUE.toLong()).toInt()
