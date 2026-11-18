package persona.feature.settings

import android.annotation.SuppressLint
import android.content.res.Resources
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import persona.core.designsystem.JRow
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.rememberCopy

/** The app resource the build writes its commit to (`defaultConfig.resValue` in app/build.gradle.kts). */
internal const val BUILD_COMMIT_RESOURCE = "persona_build_commit"

/** How many leading characters the row draws; the full commit is what Copy puts on the clipboard. */
private const val SHORT_LENGTH = 12

private val COMMIT_FORMAT = Regex("[0-9a-fA-F]{7,64}")

/** A commit the build was told about; [full] is the whole hex value, [short] what fits a row. */
internal class BuildCommit private constructor(val full: String) {
    val short: String get() = full.take(SHORT_LENGTH)

    override fun equals(other: Any?) = other is BuildCommit && other.full == full
    override fun hashCode() = full.hashCode()
    override fun toString() = "BuildCommit($short)"

    companion object {
        /** The commit in [raw]; null for a missing, empty, `unknown` or malformed value (the build knew nothing, or something else). */
        fun parse(raw: String?): BuildCommit? = raw?.takeIf { COMMIT_FORMAT.matches(it) }?.let { BuildCommit(it.lowercase()) }
    }
}

/**
 * The commit this build was made from. The settings module cannot reference the app's R, so it reads the merged resource by
 * name under the running package; no such resource, or a value that is not a commit, means the build carries none.
 */
@SuppressLint("DiscouragedApi")
internal fun Resources.buildCommit(packageName: String): BuildCommit? {
    val id = getIdentifier(BUILD_COMMIT_RESOURCE, "string", packageName)
    if (id == 0) return null
    return BuildCommit.parse(runCatching { getString(id) }.getOrNull())
}

@Composable
internal fun rememberBuildCommit(): BuildCommit? {
    val resources = LocalResources.current
    val packageName = LocalContext.current.packageName
    return remember(resources, packageName) { resources.buildCommit(packageName) }
}

/**
 * About's commit row: the short commit in the code font, and a tap copies the whole of it. A build that carries none says
 * Unknown and does nothing on tap.
 */
@Composable
internal fun AboutBuildCommitRow(commit: BuildCommit?, modifier: Modifier = Modifier) {
    val title = stringResource(R.string.settings_about_commit)
    if (commit == null) {
        JRow(title, modifier, value = stringResource(R.string.settings_about_commit_unknown))
        return
    }
    val copy = rememberCopy()
    val copied = stringResource(R.string.settings_about_commit_copied)
    JRow(modifier, onClick = { copy(commit.full, copied) }, onClickLabel = stringResource(R.string.settings_about_commit_copy),
        trailing = { Text(commit.short, style = PersonaTheme.type.codeSmall, color = PersonaTheme.palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis) }) {
        Text(title, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}
