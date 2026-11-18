package persona.feature.settings

import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import persona.core.contracts.api.AppUpdate
import persona.core.contracts.api.AppUpdateLookup
import java.io.ByteArrayOutputStream
import java.io.IOException

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class AppUpdateTransportTest {
    private val requests = mutableListOf<Request>()
    private var code = 200
    private var body = ""
    private var failure = false
    private var token = "fixture-device-token"
    private val digest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

    // Exercise the production adapter with a deterministic transport; no permissive TLS client is introduced.
    private fun api(base: String = "https://hub.example.com/persona/") = SettingsHubApi(
        OkHttpClient.Builder().addInterceptor { chain ->
            requests += chain.request()
            if (failure) throw IOException("connection interrupted")
            Response.Builder().request(chain.request()).protocol(Protocol.HTTP_1_1).code(code)
                .message("fixture").header("Location", "https://other.example.com/stolen")
                .body(body.toResponseBody()).build()
        }.build(), base.toHttpUrl(), "phone", { token }, { error("unused") }, {}, {},
    )

    @Test fun metadataAndDownloadUsePairedPrefixAndCurrentCredentials() = runBlocking {
        val adapter = api()
        body = """{"version_code":42,"version_name":"1.0.42","sha256":"$digest","notes":"Release notes","package_name":"app.example","certificate_sha256":"${"c".repeat(64)}"}"""
        val update = requireNotNull(adapter.check())
        assertEquals(42L, update.versionCode)
        assertEquals("Release notes", update.releaseNotes)
        assertEquals("app.example", update.packageName)
        assertEquals("c".repeat(64), update.certificateSha256)
        assertEquals("/persona/api/v1/app/android/latest", requests.last().url.encodedPath)
        assertEquals("Bearer fixture-device-token", requests.last().header("Authorization"))
        token = "rotated-fixture-token"
        body = "abc"
        val destination = ByteArrayOutputStream()
        adapter.download(update, destination)
        assertEquals("/persona/api/v1/app/android/apk", requests.last().url.encodedPath)
        assertEquals("Bearer rotated-fixture-token", requests.last().header("Authorization"))
        assertTrue(verifySha256(destination.toByteArray().inputStream(), update.sha256!!))
    }

    @Test fun absentReleaseIsNotAnErrorButUnauthorizedRedirectAndMalformedMetadataAre() {
        val adapter = api()
        for (status in listOf(204, 404)) {
            code = status
            assertNull(runBlocking { adapter.check() })
            assertEquals(AppUpdateLookup.NotPublished, runBlocking { adapter.lookup() })
        }
        for (status in listOf(401, 403, 302, 500)) {
            code = status
            assertThrows(IllegalStateException::class.java) { runBlocking { adapter.check() } }
            assertThrows(IllegalStateException::class.java) {
                runBlocking { adapter.download(AppUpdate(42, "42", "/api/v1/app/android/apk", digest), ByteArrayOutputStream()) }
            }
        }
        code = 200
        body = "{}"
        assertThrows(org.json.JSONException::class.java) { runBlocking { adapter.check() } }
        failure = true
        assertThrows(IOException::class.java) { runBlocking { adapter.check() } }
    }

    @Test fun plaintextAndCrossOriginDownloadsNeverReceiveCredentials() {
        val adapter = api()
        assertThrows(IllegalArgumentException::class.java) { runBlocking { api("http://hub.example.com/").check() } }
        for (url in listOf("http://hub.example.com/persona/apk", "https://other.example.com/apk", "//other.example.com/apk", "https://hub.example.com:8443/persona/apk", "https://hub.example.com/outside/apk")) {
            assertThrows(IllegalStateException::class.java) {
                runBlocking { adapter.download(AppUpdate(42, "42", url, digest), ByteArrayOutputStream()) }
            }
        }
        assertTrue(requests.isEmpty())
    }
}
