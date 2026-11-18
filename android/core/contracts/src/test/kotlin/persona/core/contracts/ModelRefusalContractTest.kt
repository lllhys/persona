package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The refusal fallback on the session and the refusal record on a notice (docs/protocol/protocol.md, #2528). */
class ModelRefusalContractTest {
    @Test fun sessionCarriesTheFallbackAndAPendingRestore() {
        val s = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"m","session_id":"s","turn_error_code":"model_fallback_unavailable",
               "model_fallback":{"subtype":"model_refusal_fallback","original_model":"primary-model","fallback_model":"fallback-model",
                 "category":"synthetic-policy","scope":"session","explanation":"Upstream refused","text":"words"},
               "model_restore":{"state":"next_start","to":"configured-model","at":"2026-11-11T08:00:00Z"}}""")
        assertEquals("model_fallback_unavailable", s.turnErrorCode)
        assertEquals(ModelRefusal("model_refusal_fallback", "primary-model", "fallback-model", "synthetic-policy", "session", "Upstream refused", "words"), s.modelFallback)
        assertEquals("next_start", s.modelRestore?.state)
        assertEquals("configured-model", s.modelRestore?.to)
    }

    @Test fun theNodesOwnRecoveryCarriesItsResetExhaustionReturnAndModelsTried() {
        // decorateModelRecovery (#7824): an exhausted chain has no fallback model; the reset is RFC3339 with an offset.
        val s = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"m","session_id":"s","model_fallback":{"subtype":"model_refusal_fallback","original_model":"primary-model",
               "category":"quota_exhausted","scope":"session","explanation":"the account's quota was exhausted",
               "recovery_reset":"2026-11-16T07:59:00+08:00","recovery_exhausted":true,"recovery_tried":"primary-model → fallback-model"}}""")
        val f = s.modelFallback!!
        assertEquals("quota_exhausted", f.category)
        assertNull(f.fallbackModel)
        assertEquals(java.time.Instant.parse("2026-11-15T23:59:00Z"), parseTime(f.recoveryReset))
        assertEquals(true, f.recoveryExhausted)
        assertEquals(false, f.recoveryReturning)
        assertEquals("primary-model → fallback-model", f.recoveryTried)
        val back = JsonCodec.decodeFromString<Session>(
            """{"session_id":"s","model_fallback":{"subtype":"model_refusal_fallback","original_model":"primary-model",
               "fallback_model":"fallback-model","category":"rate_limited","recovery_returning":true}}""").modelFallback!!
        assertEquals(true, back.recoveryReturning)
        assertEquals(false, back.recoveryExhausted)
        // A zero or malformed time is no time (D5), and the rest of the record stays.
        val zero = JsonCodec.decodeFromString<Session>(
            """{"session_id":"s","model_fallback":{"subtype":"model_refusal_fallback","fallback_model":"fallback-model",
               "recovery_reset":"0001-01-01T00:00:00Z"}}""").modelFallback!!
        assertNull(zero.recoveryReset)
        assertEquals("fallback-model", zero.fallbackModel)
    }

    @Test fun aRuntimeWrittenRecordHasNoRecoveryFields() {
        val f = JsonCodec.decodeFromString<Session>(
            """{"session_id":"s","model_fallback":{"subtype":"model_refusal_fallback","original_model":"primary-model",
               "fallback_model":"fallback-model","category":"synthetic-policy","scope":"session"}}""").modelFallback!!
        assertNull(f.recoveryReset)
        assertEquals(false, f.recoveryExhausted)
        assertEquals(false, f.recoveryReturning)
        assertNull(f.recoveryTried)
        // and none are written back for it
        val json = JsonCodec.encodeToString(ModelRefusal.serializer(), f)
        assertEquals(false, json.contains("recovery_"))
    }

    @Test fun anOlderHubsRawLineIsNotKept() {
        val it = JsonCodec.decodeFromString<Item>(
            """{"id":"n","kind":"notice","text":"words","model_refusal":{"subtype":"model_refusal_fallback","raw":"{\"secret\":1}","text":"words"}}""")
        assertEquals(ModelRefusal("model_refusal_fallback", text = "words"), it.modelRefusal)
        assertEquals(false, JsonCodec.encodeToString(Item.serializer(), it).contains("secret"))
        assertNull(JsonCodec.decodeFromString<Session>("""{"session_id":"s"}""").modelFallback)
    }
}
