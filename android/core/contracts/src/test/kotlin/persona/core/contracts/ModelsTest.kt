package persona.core.contracts

import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ModelsTest {
    @Test fun agentTaskResultsDecodeRunningAndKeepOriginalReportIdentity() {
        for (status in listOf("running", "waiting", "completed", "failed", "stopped")) {
            val item = JsonCodec.decodeFromString<Item>("""{"id":"report#0","seq":12,"role":"user","kind":"tool_result",
                "tool_use_id":"toolu_launch","text":"Full report","full_size":8192,
                "task":{"kind":"agent","status":"$status","task_id":"agent-a","tool_use_id":"toolu_launch","detail":"Preview"}}""")
            assertEquals(status, item.task?.status)
            assertEquals("toolu_launch", item.toolUseId)
            assertEquals("Full report", item.text)
            assertEquals(8192, item.fullSize)
            assertEquals(item, JsonCodec.decodeFromString<Item>(JsonCodec.encodeToString(item)))
        }
        assertNull(JsonCodec.decodeFromString<Item>("""{"id":"legacy","kind":"tool_result","text":"general-purpose says hello"}""").task)
    }

    @Test fun sessionDecodesWithMissingFieldsAndUnknownState() {
        val s = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"server","session_id":"u1","cwd":"/home/x/code/foo","status":"done","state":"later","extra":1}""",
        )
        assertEquals("foo", (s.displayTitle).zhAny())
        // D5 preserves unknown states instead of inferring a known state from status.
        assertEquals(AgentState.Unknown, s.agentState)
        assertEquals(0, s.pendingInteractions)
    }

    @Test fun zeroTimeIsUnset() {
        assertNull(parseTime("0001-01-01T00:00:00Z"))
        assertTrue(parseTime("2026-10-29T15:05:32.42Z") != null)
    }

    @Test fun offsetTimesParse() {
        // A hub in a local zone sends +08:00; the instant must match the same moment written with Z.
        assertEquals(java.time.Instant.parse("2026-11-02T00:30:00Z"), parseTime("2026-11-02T08:30:00+08:00"))
        assertEquals(java.time.Instant.parse("2026-11-02T00:30:00.5Z"), parseTime("2026-11-02T08:30:00.5+08:00"))
        assertNull(parseTime("0001-01-01T08:05:43+08:05"))
        // What a node sends (nanoseconds and the machine's own offset), and Z with nanoseconds: both one parse (#932).
        assertEquals(java.time.Instant.parse("2026-10-31T05:58:40.205893083Z"), parseTime("2026-10-31T13:58:40.205893083+08:00"))
        assertEquals(java.time.Instant.parse("2026-10-31T05:58:40.205893083Z"), parseTime("2026-10-31T05:58:40.205893083Z"))
        assertNull(parseTime("not a time"))
    }

    @Test fun resolutionOmitsUnsetFields() {
        assertEquals("""{"allow":true}""", (JsonCodec.encodeToString(Resolution(allow = true))).zhAny())
        assertEquals("""{"approve":false,"feedback":"改"}""", (JsonCodec.encodeToString(Resolution(approve = false, feedback = "改"))).zhAny())
        assertEquals("""{"option":"2"}""", (JsonCodec.encodeToString(Resolution(option = "2"))).zhAny())
    }

    @Test fun sourceFieldsDecodeAndRoundTrip() {
        val history = JsonCodec.decodeFromString<Session>(
            """{"machine_id":"m","session_id":"opaque","source_kind":"codex_history","source_thread_id":"thread",""" +
                """"source_instance_id":"instance","parent_thread_id":"parent","read_only_reason":"外部进程正在使用此会话",""" +
                """"capabilities":{"read":true,"modes":false,"future":true},"attached":true,"agent_label":"DSH"}""",
        )
        assertEquals("codex_history", (history.sourceKind).zhAny())
        assertEquals("thread", (history.sourceThreadId).zhAny())
        assertEquals("instance", (history.sourceInstanceId).zhAny())
        assertEquals("parent", (history.parentThreadId).zhAny())
        assertEquals("外部进程正在使用此会话", (history.readOnlyReason).zhAny())
        assertEquals(SessionCapabilities(read = true, modes = false), history.capabilities)
        assertTrue(history.attached)
        assertEquals(history, JsonCodec.decodeFromString<Session>(JsonCodec.encodeToString(history)))
        // A malformed capability drops only that field (D5), never the session.
        val malformed = JsonCodec.decodeFromString<Session>("""{"session_id":"s","source_kind":"runtime","capabilities":{"send":"yes","read":true}}""")
        assertEquals(SessionCapabilities(read = true), malformed.capabilities)
        val old = JsonCodec.decodeFromString<Session>("""{"session_id":"s"}""")
        assertNull(old.sourceKind); assertNull(old.capabilities); assertEquals(false, old.attached)
    }

    @Test fun agentLabelNamesEveryAgent() {
        fun label(json: String) = JsonCodec.decodeFromString<Session>("""{"session_id":"s",$json}""").agentLabel
        assertEquals("Claude", label(""""status":"idle""""))
        assertEquals("Claude", label(""""agent":"claude""""))
        assertEquals("Codex", label(""""agent":"codex""""))
        assertEquals("Codex", label(""""source_kind":"codex_app_server""""))
        assertEquals("TRAEX", label(""""agent":"traex""""))
        assertEquals("DSH", label(""""agent":"dsh","agent_label":"DSH""""))
        assertEquals("pi", label(""""agent":"pi""""))
        val dsh = JsonCodec.decodeFromString<Session>("""{"session_id":"s","agent":"dsh"}""")
        assertTrue(!dsh.isClaude && !dsh.isCodex)
        assertTrue(JsonCodec.decodeFromString<Session>("""{"session_id":"s","agent":""}""").isClaude)
    }

    @Test fun interactionRetainsRememberedPermissionScope() {
        val bash = JsonCodec.decodeFromString<Interaction>("""{"id":"ix_scope","machine_id":"computer","session_id":"chat","kind":"permission","tool_name":"Bash","always_keys":["tool:Bash"]}""")
        assertEquals(listOf("tool:Bash"), bash.alwaysKeys)
        val legacy = JsonCodec.decodeFromString<Interaction>("""{"id":"ix_legacy","machine_id":"computer","session_id":"chat","kind":"permission","tool_name":"Bash"}""")
        assertEquals(null, legacy.alwaysKeys)
    }

    @Test fun interactionNamesItsAgentAndWhatAlwaysAllows() {
        val codex = JsonCodec.decodeFromString<Interaction>(
            """{"id":"cx","kind":"permission","agent":"codex","reason":"访问网络","always_label":"以后不再问这个命令"}""",
        )
        assertEquals("Codex", (codex.agentDisplayName).zhAny())
        assertEquals("以后不再问这个命令", (codex.alwaysLabel).zhAny())
        assertEquals("访问网络", (codex.reason).zhAny())
        val dsh = JsonCodec.decodeFromString<Interaction>("""{"id":"d","kind":"question","agent":"dsh","agent_label":"DSH"}""")
        assertEquals("DSH", (dsh.agentDisplayName).zhAny())
        assertEquals("DSH 有问题问你", (dsh.headline).zhAny())
        assertEquals("DSH 请求使用 read_file：读取报表", (dsh.copy(kind = "permission", toolName = "read_file", summary = "读取报表").headline).zhAny())
        assertNull(JsonCodec.decodeFromString<Interaction>("""{"id":"c","kind":"question","agent":"claude"}""").agentDisplayName)
        assertEquals("Claude 有问题问你", (JsonCodec.decodeFromString<Interaction>("""{"id":"c","kind":"question"}""").headline).zhAny())
    }

    @Test fun sessionInfoDecodes() {
        val info = JsonCodec.decodeFromString<SessionInfo>(
            """{"session_id":"s1","cwd":"/x","model":"claude-opus","context_tokens":51234,"output_tokens":9,"turns":3,""" +
                """"compactions":1,"git_branch":"main","started_at":"2026-11-01T10:00:00+08:00","last_activity":"0001-01-01T00:00:00Z","new":1}""",
        )
        assertEquals(51234L, info.contextTokens)
        assertEquals("main", (info.gitBranch).zhAny())
        assertEquals(3, info.turns)
        assertEquals(1, info.compactions)
        assertEquals(java.time.Instant.parse("2026-11-01T02:00:00Z"), parseTime(info.startedAt))
        assertNull(info.lastActivity)
        assertEquals(SessionInfo(), JsonCodec.decodeFromString<SessionInfo>("{}"))
    }

    @Test fun interactionHeadline() {
        val ix = JsonCodec.decodeFromString<Interaction>(
            """{"id":"ix_1","kind":"permission","tool_name":"Bash","summary":"rm -rf build"}""",
        )
        assertEquals("请求使用 Bash：rm -rf build", (ix.headline).zhAny())
    }

    @Test fun personaOwnMcpServerShowsAsPersona() {
        assertEquals("Persona · agent read", toolDisplayName("mcp__persona__agent_read"))
        assertEquals("Persona · agent send", toolDisplayName("mcp__plugin_persona__agent_send"))
        assertEquals("Persona", toolDisplayName("mcp__persona"))
        assertEquals("github · create issue", toolDisplayName("mcp__github__create_issue"))
        assertEquals("persona_extra · x", toolDisplayName("mcp__persona_extra__x"))
        assertEquals("Bash", toolDisplayName("Bash"))
        assertEquals(null, toolDisplayName(null))
        assertEquals("Persona", mcpServerLabel("persona"))
        assertEquals("example-service", mcpServerLabel("example-service"))
    }

    /** Every screen that shows a request's headline names an MCP tool the way people read it, not mcp__x__y (#4806). */
    @Test fun interactionHeadlineNamesMcpTools() {
        val ix = Interaction("p", kind = "permission", toolName = "mcp__github__create_issue", summary = "Fix login")
        assertEquals("请求使用 github · create issue：Fix login", (ix.headline).zhAny())
        assertEquals("请求使用 Persona · mail send", (ix.copy(toolName = "mcp__persona__mail_send", summary = null).headline).zhAny())
        assertEquals("Codex 请求使用 github · create issue", (ix.copy(agent = "codex", summary = null).headline).zhAny())
    }
}
