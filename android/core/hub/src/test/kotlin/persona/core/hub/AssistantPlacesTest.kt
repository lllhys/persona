package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import persona.core.contracts.api.SavedPlace
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** Settings › Assistant › Saved Places (#9293): GET/POST/PUT/DELETE /api/v1/assistant/places. */
class AssistantPlacesTest {
    @Test fun missingRadiusDefaultsTo150() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"places":[
                {"id":"p1","name":"Home","address":"1 Main St","latitude":31.23,"longitude":121.47}
            ]}"""))
            val places = api.places()
            assertEquals(1, places.size)
            assertEquals("p1", places[0].id)
            assertEquals("Home", places[0].name)
            assertEquals("1 Main St", places[0].address)
            assertEquals(31.23, places[0].latitude, 0.0)
            assertEquals(121.47, places[0].longitude, 0.0)
            assertEquals(SavedPlace.DEFAULT_RADIUS, places[0].radiusMeters)
            val get = server.takeRequest()
            assertEquals("GET", get.method)
            assertEquals("/api/v1/assistant/places", get.path)
        }
    }

    @Test fun createUsesPostAndUpdateUsesPut() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody(
                """{"place":{"id":"p2","name":"Office","address":"2 Work Rd","latitude":1.0,"longitude":2.0,"radius_meters":300}}"""))
            val created = api.savePlace(SavedPlace(name = "Office", address = "2 Work Rd", latitude = 1.0, longitude = 2.0, radiusMeters = 300))
            val post = server.takeRequest()
            assertEquals("POST", post.method)
            assertEquals("/api/v1/assistant/places", post.path)
            val postBody = post.body.readUtf8()
            assertTrue(postBody.contains("\"name\":\"Office\""))
            assertTrue(postBody.contains("\"radius_meters\":300"))
            assertEquals("p2", created.id)
            assertEquals(300, created.radiusMeters)

            server.enqueue(MockResponse().setBody(
                """{"place":{"id":"p2","name":"Office","address":"2 Work Rd","latitude":1.0,"longitude":2.0,"radius_meters":500}}"""))
            val updated = api.savePlace(created.copy(radiusMeters = 500))
            val put = server.takeRequest()
            assertEquals("PUT", put.method)
            assertEquals("/api/v1/assistant/places/p2", put.path)
            assertTrue(put.body.readUtf8().contains("\"radius_meters\":500"))
            assertEquals(500, updated.radiusMeters)
        }
    }

    @Test fun deleteHitsThePlaceId() = runBlocking {
        MockWebServer().use { server ->
            val api = HttpAssistantManagement(server.url("/").toString(), "test-token", OkHttpClient())
            server.enqueue(MockResponse().setBody("""{"ok":true}"""))
            api.deletePlace("p-home")
            val delete = server.takeRequest()
            assertEquals("DELETE", delete.method)
            assertEquals("/api/v1/assistant/places/p-home", delete.path)
        }
    }
}
