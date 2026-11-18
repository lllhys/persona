package persona.core.data

import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import persona.core.domain.ContinuitySync.Action
import persona.core.rustcore.CoreHost
import persona.core.rustcore.RustCore
import persona.core.rustcore.wire.Config
import persona.core.rustcore.wire.Effect

class ShadowContinuityAdapterTest {
    @Test fun progressCountsCompletedInputsPeriodicallyAndDrainsBeforeClose() = runBlocking {
        val progress = ConcurrentLinkedQueue<Pair<Int, Int>>()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        var accepts = 0
        val adapter = ShadowContinuityAdapter("secret-chat", createShadow = { _, _ ->
            AlteredAdapter {
                if (++accepts == 10) {
                    entered.countDown()
                    check(release.await(5, TimeUnit.SECONDS))
                }
                it
            }
        }, progress = { compared, mismatches -> progress.add(compared to mismatches) })
        try {
            repeat(9) { adapter.accept(ContinuityAdapter.Input.Push(batch(it + 1))) }
            ShadowContinuityDiagnostics.awaitIdle()
            assertTrue(progress.isEmpty())
            adapter.accept(ContinuityAdapter.Input.Push(batch(10)))
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            assertTrue(progress.isEmpty())
            release.countDown()
            ShadowContinuityDiagnostics.awaitIdle()
            assertEquals(listOf(10 to 0), progress.toList())
            adapter.accept(ContinuityAdapter.Input.Push(batch(11)))
        } finally {
            release.countDown()
            adapter.close()
            adapter.close()
            ShadowContinuityDiagnostics.awaitIdle()
        }
        assertEquals(listOf(10 to 0, 11 to 0), progress.toList())
    }

    @Test fun failingProgressSinkDoesNotDisableShadowOrChangeKotlin() = runBlocking {
        val progress = ConcurrentLinkedQueue<Pair<Int, Int>>()
        val adapter = ShadowContinuityAdapter("chat", createShadow = { _, _ -> AlteredAdapter { it } },
            progress = { compared, mismatches ->
                progress.add(compared to mismatches)
                error("secret-diagnostics-error")
            })
        val plain = KotlinContinuityAdapter("chat")
        try {
            repeat(21) {
                val input = ContinuityAdapter.Input.Push(batch(it + 1))
                assertEquals(plain.accept(input), adapter.accept(input))
            }
        } finally {
            adapter.close()
            ShadowContinuityDiagnostics.awaitIdle()
        }
        assertEquals(listOf(10 to 0, 20 to 0, 21 to 0), progress.toList())
        assertEquals(0, adapter.mismatchCount)
    }

    @Test fun shadowReturnsExactlyKotlinAppliedForEveryInputKind() = runBlocking {
        val reports = ConcurrentLinkedQueue<ContinuityMismatch>()
        val plain = KotlinContinuityAdapter("private-chat")
        val adapter = ShadowContinuityAdapter("private-chat", sink = { reports.add(it) }, afterFirstFrame = { it() })
        val inputs = listOf(
            ContinuityAdapter.Input.Restore(ContinuityAdapter.State(listOf(item(1)), "private-history", 1, true, true)),
            ContinuityAdapter.Input.Latest(batch(2)),
            ContinuityAdapter.Input.Push(batch(4)),
            ContinuityAdapter.Input.After(batch(3)),
            ContinuityAdapter.Input.ContinueFill,
            ContinuityAdapter.Input.FillFailed,
            ContinuityAdapter.Input.HubChanged,
            ContinuityAdapter.Input.FillFailed,
            ContinuityAdapter.Input.Reconnect,
            ContinuityAdapter.Input.Reload(listOf(item(4)), "private-history", 4, true),
            ContinuityAdapter.Input.LoadOlder,
            ContinuityAdapter.Input.Older(listOf(item(1)), false),
            ContinuityAdapter.Input.OlderFailed,
            ContinuityAdapter.Input.Latest(ContinuityAdapter.Batch(listOf(item(4).copy(id = "private-fold", kind = "fold",
                fold = persona.core.contracts.Fold(items = 2, throughSeq = 5, throughId = "5#0"))), "private-history", toSeq = 5)),
            ContinuityAdapter.Input.OpenFold("private-fold"),
            ContinuityAdapter.Input.Range(listOf(item(4), item(5))),
            ContinuityAdapter.Input.RangeFailed(),
        )
        try {
            assertFalse(adapter.asynchronous)
            inputs.forEach { input ->
                assertEquals(plain.accept(input), adapter.accept(input))
                assertEquals(plain.state, adapter.state)
                assertEquals(plain.behind, adapter.behind)
            }
        } finally {
            adapter.close()
            ShadowContinuityDiagnostics.awaitIdle()
        }
        assertEquals(emptyList<ContinuityMismatch>(), reports.toList())
    }

    @Test fun verificationAuthorityIsTestedSeparatelyFromPairwiseState() = runBlocking {
        val driver = RustContinuityDriver(FlushContinuityFrameScheduler())
        val rust = RustContinuityAdapter("chat", driver)
        val kotlin = KotlinContinuityAdapter("chat")
        val reports = ConcurrentLinkedQueue<ContinuityMismatch>()
        val shadow = ShadowContinuityAdapter("chat", sink = { reports.add(it) }, afterFirstFrame = { it() })
        val inputs = listOf(
            ContinuityAdapter.Input.Restore(ContinuityAdapter.State(listOf(item(1)), "private-history", 1, true, true)),
            ContinuityAdapter.Input.Push(batch(2)),
            ContinuityAdapter.Input.Latest(batch(3)),
        )
        try {
            val expectedRustVerification = listOf(false, false, true)
            inputs.forEachIndexed { index, input ->
                val authoritative = kotlin.accept(input)
                val candidate = requireNotNull(rust.accept(input))
                assertTrue(authoritative.verified)
                assertEquals(expectedRustVerification[index], candidate.verified)
                assertEquals(authoritative, shadow.accept(input))
                assertEquals(authoritative.state, candidate.state)
                assertEquals(authoritative.requests, candidate.requests)
            }
        } finally {
            shadow.close()
            rust.close()
            driver.close()
            ShadowContinuityDiagnostics.awaitIdle()
        }
        assertEquals(emptyList<ContinuityMismatch>(), reports.toList())
    }

    @Test fun everyComparedFieldReportsOnlyRuleAndShape() = runBlocking {
        val reports = ConcurrentLinkedQueue<ContinuityMismatch>()
        val progress = ConcurrentLinkedQueue<Pair<Int, Int>>()
        val adapter = ShadowContinuityAdapter("secret-chat", sink = { reports.add(it) },
            progress = { compared, mismatches -> progress.add(compared to mismatches) }, createShadow = { _, _ ->
            AlteredAdapter { value -> value.copy(state = value.state.copy(
                items = listOf(item(91).copy(text = "secret-shadow-text")), history = "secret-shadow-history",
                covered = 918273, hasMore = false, loadedOnce = false), verified = false,
                requests = listOf(Action.FetchAfter(918273))) }
        })
        val input = ContinuityAdapter.Input.Latest(ContinuityAdapter.Batch(listOf(item(1).copy(
            id = "secret-item-id", text = "secret-text")), "secret-history", toSeq = 1, hasMore = true, backlog = true))
        adapter.accept(input)
        adapter.close()
        ShadowContinuityDiagnostics.awaitIdle()
        assertEquals(setOf("item-ids-order", "items", "covered", "history", "hasMore", "loadedOnce", "requests"),
            reports.map { it.rule }.toSet())
        assertEquals(7, adapter.mismatchCount)
        assertEquals(listOf(1 to 7), progress.toList())
        reports.forEach { report ->
            assertEquals("continuity", report.domain)
            assertEquals("latest items=1 history=set fromSeq=unset toSeq=set backlog=true hasMore=true moreAfter=false", report.inputShape)
            val serialized = JsonCodec.encodeToString(ContinuityMismatch.serializer(), report)
            assertTrue(serialized.contains("\"domain\":\"continuity\""))
            listOf("secret-chat", "secret-item-id", "secret-text", "secret-history", "secret-shadow-text",
                "secret-shadow-history", "918273", "91#0", "\"seq\"", "\"text\"").forEach {
                assertFalse("Diagnostics contain content: $it", serialized.contains(it))
            }
        }
    }

    @Test fun callerReturnsBeforeLatchHeldShadowAndComparisonIsOffCallerThread() = runBlocking {
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val thread = AtomicReference<Thread>()
        val adapter = ShadowContinuityAdapter("chat", createShadow = { _, _ ->
            AlteredAdapter { value ->
                thread.set(Thread.currentThread())
                entered.countDown()
                check(release.await(5, TimeUnit.SECONDS))
                value
            }
        })
        try {
            val result = adapter.accept(ContinuityAdapter.Input.Push(batch(1)))
            assertEquals(listOf(item(1)), result.state.items)
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            assertEquals(1, release.count)
            assertNotSame(Thread.currentThread(), thread.get())
            adapter.close()
            assertEquals("close must return while Rust is still blocked", 1, release.count)
        } finally {
            release.countDown()
            adapter.close()
            ShadowContinuityDiagnostics.awaitIdle()
        }
        assertEquals(0, adapter.mismatchCount)
    }

    @Test fun unusedShadowNeverCreatesNativeAndClosePreservesQueuedInputsInOrder() = runBlocking {
        val creates = AtomicInteger()
        val inputs = ConcurrentLinkedQueue<ContinuityAdapter.Input>()
        val unused = ShadowContinuityAdapter("unused", createShadow = { _, _ -> creates.incrementAndGet(); error("secret-error") })
        unused.close()
        ShadowContinuityDiagnostics.awaitIdle()
        assertEquals(0, creates.get())
        val adapter = ShadowContinuityAdapter("chat", createShadow = { _, _ ->
            creates.incrementAndGet()
            object : ContinuityAdapter by KotlinContinuityAdapter("chat") {
                private val kotlin = KotlinContinuityAdapter("chat")
                override suspend fun accept(input: ContinuityAdapter.Input, operation: Long?): ContinuityAdapter.Applied {
                    inputs.add(input)
                    return kotlin.accept(input)
                }
            }
        })
        val sent = (1..10).map { ContinuityAdapter.Input.Push(batch(it)) }
        sent.forEach { adapter.accept(it) }
        adapter.close()
        ShadowContinuityDiagnostics.awaitIdle()
        assertEquals(1, creates.get())
        assertEquals(sent, inputs.toList())
        assertEquals(0, adapter.mismatchCount)
    }

    @Test fun nativeCreationWaitsForFirstFrameWithoutBlockingKotlin() = runBlocking {
        val frameScheduled = CountDownLatch(1)
        val frame = AtomicReference<() -> Unit>()
        val creates = AtomicInteger()
        val creationThread = AtomicReference<Thread>()
        val adapter = ShadowContinuityAdapter("chat", afterFirstFrame = { frame.set(it); frameScheduled.countDown() },
            createShadow = { scheduler, rejected ->
                val driver = RustContinuityDriver(scheduler, onRejected = rejected) {
                    creates.incrementAndGet()
                    creationThread.set(Thread.currentThread())
                    RustCore(Config(), it)
                }
                object : ContinuityAdapter by RustContinuityAdapter("chat", driver) {
                    override fun close() { driver.close() }
                }
            })
        try {
            assertEquals(listOf(item(1)), adapter.accept(ContinuityAdapter.Input.Push(batch(1))).state.items)
            assertTrue(frameScheduled.await(5, TimeUnit.SECONDS))
            assertEquals(0, creates.get())
            frame.get().invoke()
            ShadowContinuityDiagnostics.awaitIdle()
            assertEquals(1, creates.get())
            assertNotSame(Thread.currentThread(), creationThread.get())
            assertEquals(0, adapter.mismatchCount)
        } finally {
            frame.get()?.invoke()
            adapter.close()
            ShadowContinuityDiagnostics.awaitIdle()
        }
    }

    @Test fun loadCreationAcceptAndRejectedFailuresDisableOnlyShadowWithoutContent() = runBlocking {
        for (failure in listOf("load", "creation", "accept", "rejected", "rejected-creation", "rejected-accept", "rejected-applied")) {
            val reports = ConcurrentLinkedQueue<ContinuityMismatch>()
            val attempts = AtomicInteger()
            val progress = ConcurrentLinkedQueue<Pair<Int, Int>>()
            val adapter = ShadowContinuityAdapter("secret-chat", sink = { reports.add(it) }, afterFirstFrame = { it() },
                progress = { compared, mismatches -> progress.add(compared to mismatches) },
                createShadow = { scheduler, rejected ->
                    attempts.incrementAndGet()
                    when (failure) {
                        "load" -> throw UnsatisfiedLinkError("secret-token")
                        "creation" -> error("secret-token")
                        "rejected-creation" -> { rejected(); error("secret-token") }
                        "rejected-accept" -> AlteredAdapter { rejected(); error("secret-token") }
                        "rejected-applied" -> AlteredAdapter { it.copy(rejected = true, diagnosticOnly = true) }
                        "rejected" -> {
                            val driver = RustContinuityDriver(scheduler, onRejected = rejected) { host ->
                                host.run(Effect.Rejected("secret-token"))
                                RustCore(Config(), host)
                            }
                            object : ContinuityAdapter by RustContinuityAdapter("secret-chat", driver) {
                                override fun close() { driver.close() }
                            }
                        }
                        else -> AlteredAdapter { error("secret-token") }
                    }
                })
            val plain = KotlinContinuityAdapter("secret-chat")
            repeat(3) {
                val input = ContinuityAdapter.Input.Push(batch(it + 1))
                assertEquals(plain.accept(input), adapter.accept(input))
            }
            adapter.close()
            ShadowContinuityDiagnostics.awaitIdle()
            assertEquals(1, attempts.get())
            assertEquals(listOf(0 to 1), progress.toList())
            val report = reports.single()
            assertEquals(when {
                failure.startsWith("rejected") -> "rust-rejected"
                failure == "load" || failure == "creation" -> "rust-creation"
                else -> "rust-failure"
            }, report.rule)
            assertFalse(JsonCodec.encodeToString(ContinuityMismatch.serializer(), report).contains("secret"))
        }
    }

    @Test fun rejectionSurvivesImplicitOpenAndInputWithinOneSynchronousAccept() = runBlocking {
        lateinit var driver: RustContinuityDriver
        var observedCount = 0L
        driver = RustContinuityDriver(FlushContinuityFrameScheduler(), onRejected = {
            observedCount = driver.rejections.count
        }) { host ->
            host.run(Effect.Rejected("secret-rejection-reason"))
            RustCore(Config(), host)
        }
        val adapter = RustContinuityAdapter("chat", driver)
        try {
            val result = requireNotNull(adapter.accept(ContinuityAdapter.Input.Push(batch(1)), operation = 42))
            assertEquals(1L, observedCount)
            assertEquals(1L, driver.rejections.count)
            assertTrue("the implicit OpenChat rejection must survive the subsequent Push apply", result.rejected)
            assertFalse("the same accept also changed the chat", result.diagnosticOnly)
            assertEquals(listOf(42L), result.operations.map { it.id })
            assertEquals(listOf(item(1)), result.state.items)
        } finally {
            adapter.close()
            driver.close()
        }
    }

    private class AlteredAdapter(private val alter: (ContinuityAdapter.Applied) -> ContinuityAdapter.Applied) : ContinuityAdapter {
        private val kotlin = KotlinContinuityAdapter("fake")
        override val state get() = kotlin.state
        override val behind get() = kotlin.behind
        override suspend fun accept(input: ContinuityAdapter.Input, operation: Long?) = alter(kotlin.accept(input))
    }
    private fun item(seq: Int) = Item("$seq#0", seq, role = "assistant", kind = "text", text = "private line")
    private fun batch(seq: Int) = ContinuityAdapter.Batch(listOf(item(seq)), "private-history", seq - 1, seq)
}
