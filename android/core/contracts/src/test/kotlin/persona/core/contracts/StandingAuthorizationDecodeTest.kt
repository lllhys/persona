package persona.core.contracts

import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class StandingAuthorizationDecodeTest {
    @Test fun missingFieldsDefaultAndARowWithoutAnIdIsDropped() {
        val list = decodeStandingAuthorizations(JsonCodec.parseToJsonElement("""{"authorizations":[
            {"id":"sa_1","actions":["merge"]},
            {"directory":"/code/skipped"},
            {"id":"","machine_id":"gone"},
            "not-an-object",
            {"id":7,"machine_id":"numeric"},
            {"id":"sa_2","machine_id":1,"directory":false,"actions":"merge","conditions":[1,"ci_green"],
                "quote":null,"source":9,"granted_at":false,"reversible":"yes"}
        ]}"""))
        assertEquals(0L, list.revision)
        assertEquals(listOf("sa_1", "sa_2"), list.authorizations.map { it.id })
        val sparse = list.authorizations[0]
        assertEquals("", sparse.machineId)
        assertEquals("", sparse.directory)
        assertEquals(listOf("merge"), sparse.actions)
        assertTrue(sparse.conditions.isEmpty())
        assertEquals("", sparse.quote)
        assertEquals("", sparse.source)
        assertNull(sparse.grantedAt)
        assertEquals(false, sparse.reversible)
        val odd = list.authorizations[1]
        assertEquals("", odd.machineId)
        assertEquals("", odd.directory)
        assertTrue(odd.actions.isEmpty())
        assertEquals(listOf("ci_green"), odd.conditions)
        assertEquals("", odd.quote)
        assertEquals("", odd.source)
        assertNull(odd.grantedAt)
        assertEquals(false, odd.reversible)
    }

    @Test fun aFullGrantKeepsItsFieldsAndTheFolderName() {
        val list = decodeStandingAuthorizations(JsonCodec.parseToJsonElement("""{"revision":4,"authorizations":[{
            "id":"sa_example","machine_id":"example-node","directory":"/code/example/","actions":["merge","push"],
            "conditions":["ci_green","no_conflicts"],"quote":"Merge after checks.","source":"Main conversation",
            "granted_at":"2026-11-11T07:36:00Z","reversible":true}]}"""))
        assertEquals(4L, list.revision)
        val grant = list.authorizations.single()
        assertEquals("example-node", grant.machineId)
        assertEquals("example", grant.folderName)
        assertEquals("/code/example/", grant.directory)
        assertEquals(listOf("merge", "push"), grant.actions)
        assertEquals(listOf("ci_green", "no_conflicts"), grant.conditions)
        assertEquals("Merge after checks.", grant.quote)
        assertEquals("Main conversation", grant.source)
        assertEquals("2026-11-11T07:36:00Z", grant.grantedAt)
        assertEquals(true, grant.reversible)
    }

    @Test fun anythingOtherThanAnObjectIsAnEmptyList() {
        assertEquals(StandingAuthorizationList(), decodeStandingAuthorizations(JsonPrimitive("nope")))
        assertEquals(StandingAuthorizationList(), decodeStandingAuthorizations(JsonCodec.parseToJsonElement("{}")))
    }
}
