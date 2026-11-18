package persona.feature.settings

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.GitHubAuth
import persona.core.contracts.api.GitHubInboxOptions
import persona.core.contracts.api.GitHubMachine
import persona.core.contracts.api.GitHubMissing

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class GitHubConnectorTextTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources

    private fun machine(state: String, account: String? = null, missing: List<GitHubMissing>? = null, error: String? = null) =
        GitHubMachine(id = "studio", name = "Studio", state = state, account = account, missing = missing, error = error)

    @Test
    fun eachMachineState() {
        assertEquals("gh signed in", GitHubConnectorText.machineSubtitle(res, machine("signed_in")))
        assertEquals("gh signed in · octo", GitHubConnectorText.machineSubtitle(res, machine("signed_in", account = "octo")))
        val missing = listOf(GitHubMissing("repo"), GitHubMissing("notifications"))
        assertEquals(
            "gh signed in · octo · missing repo, notifications scope",
            GitHubConnectorText.machineSubtitle(res, machine("signed_in", account = "octo", missing = missing)),
        )
        assertEquals("gh not signed in", GitHubConnectorText.machineSubtitle(res, machine("not_signed_in")))
        assertEquals("gh not installed", GitHubConnectorText.machineSubtitle(res, machine("no_gh")))
        assertEquals("personad is too old to read gh sign-in", GitHubConnectorText.machineSubtitle(res, machine("too_old")))
        assertEquals("Offline", GitHubConnectorText.machineSubtitle(res, machine("offline")))
        assertEquals("Can't read gh sign-in: boom", GitHubConnectorText.machineSubtitle(res, machine("error", error = "boom")))
        assertEquals("Can't read gh sign-in", GitHubConnectorText.machineSubtitle(res, machine("error")))
        assertEquals("Can't read gh sign-in", GitHubConnectorText.machineSubtitle(res, machine("other")))
        assertFalse(GitHubConnectorText.warns(machine("signed_in")))
        assertTrue(GitHubConnectorText.warns(machine("signed_in", missing = missing)))
        assertTrue(GitHubConnectorText.warns(machine("offline")))
    }

    @Test
    fun missingAdviceNamesTheSource() {
        val missing = listOf(GitHubMissing("repo"), GitHubMissing("notifications"))
        val device = GitHubAuth(source = "device", missing = missing)
        assertEquals(
            "Device code sign-in is missing the repo, notifications scope: switch to another sign-in method, then sign in with a device code again.",
            GitHubConnectorText.missingAdvice(res, device, missing),
        )
        val named = GitHubAuth(source = "machine", machine = "studio", machines = listOf(GitHubMachine(id = "studio", name = "Studio")))
        assertEquals(
            "gh on Studio is missing the repo, notifications scope: run gh auth refresh -s repo,notifications on that computer, or use device code sign-in.",
            GitHubConnectorText.missingAdvice(res, named, missing),
        )
        val unnamed = GitHubAuth(source = "machine", machine = "gone")
        assertTrue(GitHubConnectorText.missingAdvice(res, unnamed, listOf(GitHubMissing("repo"))).startsWith("gh on This computer is missing the repo scope:"))
    }

    @Test
    fun inboxFooterUsesTheModeAndTheDefaultBranches() {
        val mine = GitHubConnectorText.inboxFooter(res, GitHubInboxOptions(ci = "mine"))
        assertTrue(mine.startsWith("CI failures on the default branch and your open PRs count as Needs you; other branches are FYI."))
        assertTrue(mine.contains(" Automated branches (train/*) aren't flagged."))
        assertTrue(mine.contains(" @mentions, review requests,"))
        val off = GitHubConnectorText.inboxFooter(res, GitHubInboxOptions(ci = "off", ignoreBranches = listOf("train/*", "renovate/*")))
        assertTrue(off.startsWith("CI failures don't go to the Inbox."))
        assertTrue(off.contains("Automated branches (train/*, renovate/*) aren't flagged."))
        assertEquals("Couldn't save: nope", res.getString(R.string.settings_gh_couldnt_save, "nope"))
    }

    @Test
    @Config(qualifiers = "zh")
    fun simplifiedChineseSignedIn() {
        assertEquals("gh 已登录 · octo", GitHubConnectorText.machineSubtitle(res, machine("signed_in", account = "octo")))
        assertEquals("、", res.getString(R.string.settings_gh_list_separator))
        assertEquals("离线", GitHubConnectorText.machineSubtitle(res, machine("offline")))
    }

    @Test
    @Config(qualifiers = "ja")
    fun japaneseOfflineAndSeparator() {
        assertEquals("オフライン", GitHubConnectorText.machineSubtitle(res, machine("offline")))
        assertEquals("、", res.getString(R.string.settings_gh_list_separator))
        val footer = GitHubConnectorText.inboxFooter(res, GitHubInboxOptions())
        assertFalse(footer.startsWith(" "))
        assertTrue(footer.contains("自動ブランチ（train/*）は通知しません。"))
    }
}
