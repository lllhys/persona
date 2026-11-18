package persona.feature.settings

import android.content.Context
import android.content.ClipboardManager
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.click
import androidx.compose.ui.test.swipeDown
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.filterToOne
import androidx.compose.ui.test.hasAnyAncestor
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.isDialog
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTouchInput
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.CatalogExtension
import persona.core.contracts.api.ExtensionDetail
import persona.core.contracts.api.ExtensionRemoval
import persona.core.contracts.api.HubExtension
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import java.io.File

/** Cleanup warnings after a successful uninstall: three languages, every style and appearance, and both removal pages. */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ExtensionRemovalWarningsShotsTest {
    @get:Rule val rule = createComposeRule()

    private val warnings = listOf(
        "Delete /Users/example/.persona/extensions/example/data/" + "cache/".repeat(14) + "index.db manually.",
        "The scheduled-task file remains; remove it manually. Cleanup token: " + "x".repeat(240),
        "The package directory could not be removed; use the hub machine to finish its cleanup.",
    )

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/extension-removal-warning-shots").also { it.mkdirs() }

    private fun capture(name: String) {
        rule.waitForIdle()
        val image = rule.onNode(isDialog()).captureToImage().asAndroidBitmap()
        assertTrue(image.width > 0 && image.height > 0)
        File(out(), "$name.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
    @Test fun english() = sheet(
        "en", "Removed Example Notes", "Removed. A few things still need you to clean them up by hand.",
        "Needs manual cleanup", "Copy warnings", "OK",
    )

    @Config(sdk = [35], qualifiers = "zh-w400dp-h1200dp-xhdpi")
    @Test fun simplifiedChinese() = sheet(
        "zh", "已卸载 Example Notes", "已卸载。还有一些项目需要你手动清理。", "需要手动清理", "复制全部警告", "好",
    )

    @Config(sdk = [35], qualifiers = "ja-w400dp-h1200dp-xhdpi")
    @Test fun japanese() = sheet(
        "ja", "Example Notesをアンインストールしました", "アンインストールは完了しました。一部の項目は手動でクリーンアップする必要があります。",
        "手動クリーンアップが必要な項目", "警告をすべてコピー", "OK",
    )

    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
    @Test fun singleWarning() = sheet(
        "en", "Removed Example Notes", "Removed. A few things still need you to clean them up by hand.",
        "Needs manual cleanup", "Copy warnings", "OK", items = listOf(warnings.first()), state = "single",
    )

    private fun sheet(
        language: String, title: String, body: String, heading: String, copyLabel: String, doneLabel: String,
        items: List<String> = warnings, state: String = "multiple-long",
    ) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var done = 0
        rule.setContent {
            PersonaTheme(style = style, darkTheme = dark, reduceMotion = true) {
                ExtensionRemovalWarningsSheet(ExtensionRemovalNotice(title, items), onDone = { done++ })
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s
            dark = d
            rule.waitForIdle()
            rule.onNodeWithText(title).assertExists()
            rule.onNodeWithText(body).assertExists()
            rule.onNodeWithText(heading).assertExists()
            items.forEach { rule.onNodeWithText(it).assertExists() }
            rule.onNodeWithTag("extension-removal-warning-0").assertExists()
            capture("extension-removal-warnings-$language-$state-${s.name.lowercase()}-${if (d) "dark" else "light"}")
        }
        rule.onNodeWithText(copyLabel).performClick()
        assertEquals(0, done)
        assertEquals(items.joinToString("\n"), clipboardText())
        rule.onNodeWithText(doneLabel).performClick()
        assertEquals(1, done)
    }

    private fun clipboardText(): String? {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        return clipboard.primaryClip?.getItemAt(0)?.text?.toString()
    }

    @Config(sdk = [35], qualifiers = "en-w360dp-h640dp-xhdpi", fontScale = 2f)
    @Test fun longWarningsScrollToTheEndAndKeepCopyAndDoneReachable() {
        val items = (0 until 8).map { index ->
            "Cleanup item $index: " + "x".repeat(180) + " remains under /Users/example/.persona/extensions/example/data/"
        }
        var done = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ExtensionRemovalWarningsSheet(ExtensionRemovalNotice("Removed Example Notes", items), onDone = { done++ })
            }
        }
        rule.onNodeWithTag("extension-removal-warning-${items.lastIndex}").performScrollTo().assertIsDisplayed()
        capture("extension-removal-warnings-en-multiple-long-compact-large-type")
        rule.onNodeWithText("Copy warnings").performClick()
        assertEquals(items.joinToString("\n"), clipboardText())
        assertEquals(0, done)
        rule.onNodeWithText("OK").performClick()
        assertEquals(1, done)
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
    @Test fun dragScrimAndBackDoNotDismissTheWarnings() {
        var done = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                ExtensionRemovalWarningsSheet(ExtensionRemovalNotice("Removed Example Notes", warnings), onDone = { done++ })
            }
        }
        rule.onNodeWithTag("extension-removal-warnings").performTouchInput { swipeDown() }
        rule.waitForIdle()
        rule.onNodeWithText("Removed Example Notes").assertIsDisplayed()
        rule.onNode(isDialog()).performTouchInput { click(androidx.compose.ui.geometry.Offset(center.x, 1f)) }
        rule.waitForIdle()
        rule.onNodeWithText("Removed Example Notes").assertIsDisplayed()
        rule.runOnIdle { org.robolectric.shadows.ShadowDialog.getLatestDialog().onBackPressed() }
        rule.waitForIdle()
        rule.onNodeWithText("Removed Example Notes").assertIsDisplayed()
        assertEquals(0, done)
        rule.onNodeWithText("OK").performClick()
        assertEquals(1, done)
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h1200dp-xhdpi")
    @Test fun confirmationSeparatesAutomaticDeletionRetainedDataAndManualCleanup() {
        var dismissed = 0
        val confirmed = mutableListOf<Pair<Boolean, Boolean>>()
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                UninstallDialog("Example Notes", onDismiss = { dismissed++ }, installedVersion = "1.2.0") { keep, members ->
                    confirmed += keep to members
                }
            }
        }
        rule.onNodeWithText("The package will be deleted too; the data folder will stay on the hub machine.", substring = true).assertExists()
        rule.onNodeWithText("Anything still needing manual cleanup will be listed after removal", substring = true).assertExists()
        rule.onNodeWithText("the extension is still removed", substring = true).assertDoesNotExist()
        rule.onNodeWithTag("uninstall-keep-data").performClick()
        rule.onNodeWithText("The package and data folder will be deleted too.", substring = true).assertExists()
        rule.onNodeWithText("Cancel").performClick()
        assertEquals(1, dismissed)
        assertTrue(confirmed.isEmpty())
        rule.onAllNodesWithText("Remove").filterToOne(hasAnyAncestor(isDialog()) and hasClickAction()).performClick()
        assertEquals(listOf(false to false), confirmed)
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h1400dp-xhdpi")
    @Test fun marketRemovalShowsOnlyReturnedWarnings() {
        val api = object : FakeOperations(stepped = false) {
            override suspend fun extension(id: String) = ExtensionDetail(HubExtension(id, "Example Notes", "ui", "running"))
        }.apply {
            catalog = listOf(
                CatalogExtension(
                    "m1", id = "example-notes", name = "Example Notes", version = "1.2.0", installed = "1.2.0",
                    installedSource = "installed", fromThisMarket = true,
                ),
            )
            removal = ExtensionRemoval(warnings = warnings)
        }
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionCatalogPage(api, "m1", "example-notes", onBack = {}, onInstalled = {})
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("Remove").fetchSemanticsNodes().isNotEmpty() }
        rule.onAllNodesWithText("Remove").filterToOne(hasClickAction()).performClick()
        rule.onNodeWithText("Cancel").performClick()
        assertFalse(api.calls.any { it == "uninstall" })

        rule.onAllNodesWithText("Remove").filterToOne(hasClickAction()).performClick()
        rule.onAllNodesWithText("Remove").filterToOne(hasAnyAncestor(isDialog()) and hasClickAction()).performClick()
        rule.onNodeWithText("Removed Example Notes").assertExists()
        rule.onNodeWithText("Needs manual cleanup").assertExists()
        rule.onNodeWithText("OK").performClick()
        rule.waitForIdle()
        rule.onNode(isDialog()).assertDoesNotExist()

        api.removal = ExtensionRemoval()
        rule.onAllNodesWithText("Remove").filterToOne(hasClickAction()).performClick()
        rule.onAllNodesWithText("Remove").filterToOne(hasAnyAncestor(isDialog()) and hasClickAction()).performClick()
        rule.waitForIdle()
        rule.onNode(isDialog()).assertDoesNotExist()
        assertEquals(2, api.calls.count { it == "uninstall" })
    }

    @Config(sdk = [35], qualifiers = "en-w400dp-h1600dp-xhdpi")
    @Test fun installedRemovalHonorsWarningsAndEmptyResults() {
        val detail = ExtensionDetail(
            HubExtension("example-notes", "Example Notes", "connectors", "running", source = "installed", enabled = true),
            origin = "market:m1/example-notes",
        )
        val api = object : ExtensionApiStub() {
            var removal = ExtensionRemoval(warnings = warnings)
            var removals = 0
            override suspend fun extensions() = listOf(detail.extension)
            override suspend fun extension(id: String) = detail
            override suspend fun uninstall(id: String, keepData: Boolean, cascade: Boolean, members: Boolean): ExtensionRemoval {
                removals++
                return removal
            }
        }
        var backs = 0
        rule.setContent {
            PersonaTheme(reduceMotion = true) {
                Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background)) {
                    ExtensionPage(api, "example-notes", onBack = { backs++ })
                }
            }
        }
        rule.waitUntil { rule.onAllNodesWithText("Example Notes").fetchSemanticsNodes().isNotEmpty() }
        rule.onNodeWithText("Remove").performScrollTo().performClick()
        rule.onAllNodesWithText("Remove").filterToOne(hasAnyAncestor(isDialog()) and hasClickAction()).performClick()
        rule.onNodeWithText("Removed Example Notes").assertExists()
        assertEquals(0, backs)
        rule.onNodeWithText("OK").performClick()
        rule.waitForIdle()
        assertEquals(1, api.removals)
        assertEquals(1, backs)

        api.removal = ExtensionRemoval()
        rule.onNodeWithText("Remove").performScrollTo().performClick()
        rule.onAllNodesWithText("Remove").filterToOne(hasAnyAncestor(isDialog()) and hasClickAction()).performClick()
        rule.waitForIdle()
        assertEquals(2, api.removals)
        assertEquals(2, backs)
        rule.onNode(isDialog()).assertDoesNotExist()
    }
}
