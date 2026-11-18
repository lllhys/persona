package persona.feature.settings

import android.graphics.Bitmap
import androidx.activity.ComponentActivity
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.Density
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.AppUpdate
import persona.core.contracts.api.AppUpdates
import persona.core.designsystem.AppStyle
import persona.core.designsystem.AppStyleStore
import persona.core.designsystem.LocalAppStyleStore
import persona.core.designsystem.LocalLanguageHost
import persona.core.designsystem.PersonaTheme
import java.io.File

/** The real settings screen renders an available release without starting installation. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class AppUpdateShotsTest {
    @get:Rule val rule = createAndroidComposeRule<ComponentActivity>()

    @Test fun availableReleaseOffersUserInitiatedInstallation() {
        val services = demoSettingsServices(rule.activity.resources).copy(
            demo = false,
            updates = object : AppUpdates {
                override suspend fun check() = AppUpdate(
                    2_000_000_000, "1.0.2000000000", "/api/v1/app/android/apk",
                    "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
                    "自动发布版本 · 下载校验后由系统确认安装",
                )
            },
        )
        rule.setContent {
            val store = remember { AppStyleStore(rule.activity) }
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                CompositionLocalProvider(LocalLanguageHost provides rule.activity, LocalAppStyleStore provides store) {
                    Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("update-screen")) {
                        SettingsScreen(services)
                    }
                }
            }
        }
        rule.waitForIdle()
        val about = rule.activity.getString(R.string.settings_home_about)
        rule.onNode(hasScrollAction()).performScrollToNode(hasText(about))
        rule.onNodeWithText(about).performClick()
        val install = rule.activity.getString(R.string.settings_update_install)
        rule.onNode(hasScrollAction()).performScrollToNode(hasText(install))
        rule.onNodeWithText(install).assertExists()
        val image = rule.onNodeWithTag("update-screen").captureToImage().asAndroidBitmap()
        val output = File("build/update-shots/settings-available-update.png")
        check(output.parentFile!!.mkdirs() || output.parentFile!!.isDirectory)
        output.outputStream().use { check(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
    }

    @Test fun failedCheckIsVisibleAlongsideCachedUpdate() {
        val release = AppUpdate(101, "1.0.101", "/api/v1/app/android/apk", "a".repeat(64))
        val api = object : AppUpdates {
            override suspend fun check(): AppUpdate? = throw IllegalStateException("offline")
        }
        val controller = AppUpdateState(api, 100, initialLatest = release, initialResult = AppUpdateState.Result.Failed)
        val services = demoSettingsServices(rule.activity.resources).copy(demo = false, updates = api, appUpdateState = controller)
        rule.setContent {
            val store = remember { AppStyleStore(rule.activity) }
            PersonaTheme(darkTheme = false, reduceMotion = true) {
                CompositionLocalProvider(LocalLanguageHost provides rule.activity, LocalAppStyleStore provides store) {
                    SettingsScreen(services, initialPage = "about")
                }
            }
        }
        rule.onNodeWithText(rule.activity.getString(R.string.settings_update_check_failed)).assertExists()
        rule.onNodeWithText(rule.activity.getString(R.string.settings_update_install)).assertExists()
    }

    @Test fun signatureGuidanceAndCheckSourceRenderInEveryStyle() {
        val certificate = "c".repeat(64)
        val api = object : AppUpdates {
            override suspend fun check() = AppUpdate(101, "1.0.101", "/api/v1/app/android/apk",
                "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
                packageName = "app.example", certificateSha256 = certificate)
        }
        val controller = AppUpdateState(api, 100, installedPackage = "app.example", installedCertificates = setOf("d".repeat(64)))
        val services = demoSettingsServices(rule.activity.resources).copy(demo = false, updates = api,
            appUpdateState = controller, hubAddress = "https://hub.example.com")
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var fontScale by mutableStateOf(1f)
        rule.setContent {
            val store = remember { AppStyleStore(rule.activity) }
            val density = LocalDensity.current
            CompositionLocalProvider(LocalDensity provides Density(density.density, fontScale)) {
                PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                    CompositionLocalProvider(LocalLanguageHost provides rule.activity, LocalAppStyleStore provides store) {
                        Box(Modifier.fillMaxSize().background(PersonaTheme.palette.background).testTag("update-screen")) {
                            SettingsScreen(services, initialPage = "about")
                        }
                    }
                }
            }
        }
        rule.waitForIdle()
        val detail = rule.activity.getString(R.string.settings_update_signature_mismatch_detail)
        rule.onNode(hasScrollAction()).performScrollToNode(hasText(detail))
        rule.onNodeWithText(detail).assertExists()
        for (nextStyle in AppStyle.entries) for (scale in listOf(1f, 2f)) for (nextDark in listOf(false, true)) {
            style = nextStyle
            dark = nextDark
            fontScale = scale
            rule.waitForIdle()
            val image = rule.onNodeWithTag("update-screen").captureToImage().asAndroidBitmap()
            val name = "settings-signature-mismatch-${nextStyle.name.lowercase()}-${if (nextDark) "dark" else "light"}-${if (scale == 1f) "default" else "large"}.png"
            val output = File("build/update-shots", name)
            check(output.parentFile!!.mkdirs() || output.parentFile!!.isDirectory)
            output.outputStream().use { check(image.compress(Bitmap.CompressFormat.PNG, 100, it)) }
        }
    }
}
