package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.DoneNotification
import persona.core.contracts.api.HubTranslatePrefs
import persona.core.contracts.api.QuietHours
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.boolean
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpDeviceSettingsTest {
    @Test fun deviceEndpointsAndPartialPreferenceWritePreserveOtherSettings() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpDeviceSettings(server.url("/").toString(), "example-token", "phone/id", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"current":"credential","tokens":[{"id":"credential","name":"示例手机","kind":"phone","role":"full","last_seen":"2026-01-01T00:00:00Z"},{"id":"revoked","revoked_at":"2026-01-02T00:00:00Z"}]}"""))
            val devices = api.devices()
            assertEquals("credential", (devices.currentId).zhAny())
            assertEquals("2026-01-01T00:00:00Z", (devices.devices.single().lastSeen).zhAny())
            assertNull(devices.devices.single().platform)
            assertEquals("/api/v1/auth/tokens", (server.takeRequest().path).zhAny())

            val prefs = """{"input":true,"done":"long","lock_details":false,"island":{"preset":"custom"},"long_turn_seconds":120,"done_delay_seconds":60}"""
            server.enqueue(MockResponse().setBody(prefs))
            val original = api.preferences()
            assertEquals(120, original.longTurnSeconds)
            assertTrue(original.liveActivity)
            val read = server.takeRequest()
            assertEquals("/api/v1/devices/phone%2Fid/prefs", (read.path).zhAny())
            assertEquals("Bearer example-token", (read.getHeader("Authorization")).zhAny())
            server.enqueue(MockResponse().setBody(prefs.replace("\"long\"", "\"always\"").replace("\"input\":true", "\"input\":true,\"live_activity\":false")))
            val saved = api.savePreferences(original.copy(done = DoneNotification.Always, liveActivity = false))
            assertEquals(DoneNotification.Always, saved.done)
            assertFalse(saved.liveActivity)
            val update = server.takeRequest()
            assertEquals("PUT", (update.method).zhAny())
            val body = JsonCodec.parseToJsonElement(update.body.readUtf8()).jsonObject
            assertEquals(setOf("input", "done", "lock_details", "live_activity"), body.keys)
            assertFalse(body.getValue("live_activity").jsonPrimitive.boolean)

            server.enqueue(MockResponse().setResponseCode(204))
            api.revoke("credential/id")
            val revoke = server.takeRequest()
            assertEquals("DELETE", (revoke.method).zhAny())
            assertEquals("/api/v1/auth/tokens/credential%2Fid", (revoke.path).zhAny())
            server.enqueue(MockResponse().setResponseCode(302).setHeader("Location", "https://example.com/redirect"))
            try { api.preferences(); fail("Redirect must be rejected") } catch (_: HubFailure) { }
            server.takeRequest()
            assertEquals(5, server.requestCount)
        }
    }

    @Test fun translatePrefsAreReadAndWrittenWithEveryField() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpDeviceSettings(server.url("/").toString(), "example-token", "phone", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","translate":{"enabled":true,"target":"en","mail":true}}"""))
            assertEquals(HubTranslatePrefs(true, "en", true), api.preferences().translate)
            server.takeRequest()
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","translate":{"enabled":false,"target":"zh-Hans","mail":false}}"""))
            assertEquals(HubTranslatePrefs(false, "zh-Hans", false), api.saveTranslatePrefs(HubTranslatePrefs(false, "zh-Hans", false)))
            val put = server.takeRequest()
            assertEquals("PUT", (put.method).zhAny())
            val body = JsonCodec.parseToJsonElement(put.body.readUtf8()).jsonObject
            assertEquals(setOf("translate"), body.keys)
            val translate = body.getValue("translate").jsonObject
            assertEquals(setOf("enabled", "target", "mail"), translate.keys)
            assertFalse(translate.getValue("enabled").jsonPrimitive.boolean)
        }
    }

    @Test fun anOlderHubHasNoTranslatePrefs() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpDeviceSettings(server.url("/").toString(), "example-token", "phone", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long"}"""))
            assertNull(api.preferences().translate)
        }
    }

    @Test fun mutedManagersAreReadAndWrittenAlone() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpDeviceSettings(server.url("/").toString(), "example-token", "phone", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","muted_managers":["m-2"]}"""))
            assertEquals(listOf("m-2"), api.preferences().mutedManagers)
            server.takeRequest()
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","muted_managers":["m-2","m-3"]}"""))
            assertEquals(listOf("m-2", "m-3"), api.saveMutedManagers(listOf("m-2", "m-3", "m-2")))
            val put = server.takeRequest()
            assertEquals("PUT", put.method)
            assertEquals("/api/v1/devices/phone/prefs", put.path)
            val body = JsonCodec.parseToJsonElement(put.body.readUtf8()).jsonObject
            // A partial update: only the list, so the other switches stay as they are.
            assertEquals(setOf("muted_managers"), body.keys)
            assertEquals(listOf("m-2", "m-3"), (body.getValue("muted_managers") as kotlinx.serialization.json.JsonArray).map { it.jsonPrimitive.content })
            // 推送通知 changes nothing else: the whole-prefs save leaves the list out.
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","muted_managers":[]}"""))
            api.savePreferences(persona.core.contracts.api.DeviceNotificationPreferences(mutedManagers = listOf("m-9")))
            assertFalse("muted_managers" in JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject.keys)
            // An older hub: no list.
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long"}"""))
            assertNull(api.preferences().mutedManagers)
        }
    }

    @Test fun quietHoursAreReadAndWrittenAlone() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpDeviceSettings(server.url("/").toString(), "example-token", "phone", OkHttpClient())
            // Never set: the hub's empty times read as the defaults.
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","quiet":{"enabled":false,"start":"","end":""}}"""))
            assertEquals(QuietHours(), api.preferences().quiet)
            server.takeRequest()
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","quiet":{"enabled":true,"start":"22:30","end":"06:45","tz":"Asia/Tokyo"}}"""))
            assertEquals(QuietHours(true, "22:30", "06:45", "Asia/Tokyo"), api.saveQuietHours(QuietHours(true, "22:30", "06:45", "Asia/Tokyo")))
            val put = server.takeRequest()
            assertEquals("PUT", put.method)
            assertEquals("/api/v1/devices/phone/prefs", put.path)
            val body = JsonCodec.parseToJsonElement(put.body.readUtf8()).jsonObject
            // A partial update: only the window, so the other switches stay as they are.
            assertEquals(setOf("quiet"), body.keys)
            val quiet = body.getValue("quiet").jsonObject
            assertEquals(setOf("enabled", "start", "end", "tz"), quiet.keys)
            assertTrue(quiet.getValue("enabled").jsonPrimitive.boolean)
            assertEquals("06:45", quiet.getValue("end").jsonPrimitive.content)
            // The whole-prefs save leaves the window out.
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long","quiet":{"enabled":true,"start":"22:30","end":"06:45"}}"""))
            api.savePreferences(persona.core.contracts.api.DeviceNotificationPreferences(quiet = QuietHours()))
            assertFalse("quiet" in JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject.keys)
            // An older hub: no window, so no section.
            server.enqueue(MockResponse().setBody("""{"input":true,"done":"long"}"""))
            assertNull(api.preferences().quiet)
        }
    }
}
