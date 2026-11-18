package persona.core.domain.answer

import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import persona.core.contracts.Interaction
import persona.core.contracts.JsonCodec
import persona.core.contracts.Question
import persona.core.contracts.Resolution
import persona.core.rustcore.RustCoreAnswer

/** Only counts, flags and operation enums cross this diagnostics boundary; never labels, questions, answers or errors. */
@Serializable
data class AnswerMismatch(
    val domain: String,
    val rule: String,
    val inputShape: String,
)

/** Process-wide counters are read-only diagnostics. Each adapter owns its own bounded queue, not a shared content queue. */
object ShadowAnswerDiagnostics {
    private val compared = AtomicInteger()
    private val mismatches = AtomicInteger()
    val comparedCount: Int get() = compared.get()
    val mismatchCount: Int get() = mismatches.get()

    fun report(mismatch: AnswerMismatch) {
        mismatches.incrementAndGet()
    }

    internal fun compared() { compared.incrementAndGet() }

    /** Test-only counter reset; production diagnostics never call this. */
    internal fun resetForTest() {
        compared.set(0)
        mismatches.set(0)
    }
}

internal class BoundedAnswerWorker {
    private val capacity = 32
    private val worker = ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS, LinkedBlockingQueue(capacity)) { runnable ->
        Thread(runnable, "PersonaAnswerShadow").apply { isDaemon = true }
    }.apply {
        rejectedExecutionHandler = ThreadPoolExecutor.DiscardPolicy()
    }
    private val dropped = AtomicLong()
    val droppedCount: Long get() = dropped.get()
    val queuedCount: Int get() = worker.queue.size
    @Volatile var testDrained: (() -> Unit)? = null

    fun enqueue(work: () -> Unit): Boolean {
        if (worker.queue.size >= capacity) {
            dropped.incrementAndGet()
            return false
        }
        return try {
            worker.execute {
                work()
                testDrained?.invoke()
            }
            true
        } catch (_: Throwable) {
            dropped.incrementAndGet()
            false
        }
    }

    /** Test barrier only; it waits for this adapter, never for an unrelated instance's content. */
    fun awaitIdle() {
        val completed = worker.submit(java.util.concurrent.Callable { true })
        check(completed.get(30, TimeUnit.SECONDS)) { "The answer shadow barrier was rejected" }
    }
}

/**
 * Kotlin remains authoritative. Unsupported operations, malformed native output and native failures are diagnostic
 * outcomes only. The adapter never substitutes a Rust result for the result the current UI already received.
 */
class ShadowAnswerAdapter(
    private val kotlin: AnswerAdapter = KotlinAnswerAdapter(),
    private val projectRust: (String) -> String = RustCoreAnswer::project,
    private val displayRust: (String) -> String = RustCoreAnswer::display,
    private val sink: ((AnswerMismatch) -> Unit)? = null,
    private val progress: (compared: Int, mismatches: Int) -> Unit = { _, _ -> },
) : AnswerAdapter {
    private val worker = BoundedAnswerWorker()
    private val compared = AtomicInteger()
    private val mismatches = AtomicInteger()
    private val seenDisplays = java.util.concurrent.ConcurrentHashMap.newKeySet<String>()
    val mismatchCount: Int get() = mismatches.get()
    val droppedCount: Long get() = worker.droppedCount
    val queuedCount: Int get() = worker.queuedCount
    internal var testDrained: (() -> Unit)?
        get() = worker.testDrained
        set(value) { worker.testDrained = value }

    /** Test-only barrier for this adapter's own queue. */
    fun awaitIdle() { worker.awaitIdle() }

    override fun build(interaction: Interaction, action: AnswerAction, draft: AnswerDraft, context: AnswerContext): AnswerResult {
        val result = kotlin.build(interaction, action, draft, context)
        val shape = shape(interaction, action, draft, context)
        worker.enqueue {
            try { compareBuild(interaction, action, draft, context, result, shape) }
            catch (_: Throwable) { report("shadow-failure", shape) }
        }
        return result
    }

    override fun display(label: String): OptionDisplay {
        val result = kotlin.display(label)
        // Recomposition repeats the same labels. Compare each label once per adapter instead of retaining duplicates.
        if (!seenDisplays.add(label)) return result
        val shape = "labelChars=${label.length}"
        val queued = worker.enqueue {
            try { compareDisplay(label, result, shape) }
            catch (_: Throwable) {
                seenDisplays.remove(label)
                report("shadow-failure", shape)
            }
        }
        if (!queued) seenDisplays.remove(label)
        return result
    }

    private fun compareBuild(
        interaction: Interaction,
        action: AnswerAction,
        draft: AnswerDraft,
        context: AnswerContext,
        expected: AnswerResult,
        shape: String,
    ) {
        if (context.state != "pending" || context.locked || context.rejecting || context.fullPlanRead ||
            draft != AnswerDraft(page = draft.page, picks = draft.picks, other = draft.other, optionIndex = draft.optionIndex)
        ) {
            report("unsupported-context", shape)
            return
        }
        if (action !in setOf(AnswerAction.PickOption, AnswerAction.Advance, AnswerAction.Submit) || interaction.kind != "question") {
            report("unsupported-operation", shape)
            return
        }
        // Notification answers enforce a separate one-question/max-options policy not represented by the pure core.
        if (context.entry != AnswerEntry.Conversation) {
            report("unsupported-entry", shape)
            return
        }
        val input = try { encodeBuild(interaction, action, draft, context) } catch (_: Throwable) {
            report("input-conversion", shape); return
        }
        val raw = try { projectRust(input.toString()) } catch (_: Throwable) {
            report("rust-failure", shape); return
        }
        val actual = try {
            if (raw.isEmpty()) throw IllegalArgumentException("empty native output")
            JsonCodec.decodeFromString<CoreTransition>(raw)
        } catch (_: Throwable) {
            report("rust-output", shape); return
        }
        compareTransition(action, expected, actual, shape)
        completed()
    }

    private fun compareDisplay(label: String, expected: OptionDisplay, shape: String) {
        val raw = try { displayRust(label) } catch (_: Throwable) {
            seenDisplays.remove(label)
            report("rust-failure", shape); return
        }
        val actual = try {
            if (raw.isEmpty()) throw IllegalArgumentException("empty native output")
            JsonCodec.decodeFromString<OptionDisplay>(raw)
        } catch (_: Throwable) {
            seenDisplays.remove(label)
            report("rust-output", shape); return
        }
        if (expected != actual) report("display", shape)
        completed()
    }

    private fun compareTransition(action: AnswerAction, expected: AnswerResult, actual: CoreTransition, shape: String) {
        val page = expected.nextPage ?: expected.draft.page
        if (expected.allowed != actual.allowed) report("allowed", shape)
        if (page != actual.page) report("page", shape)
        // Advance can have no resolution yet: every supported operation must protect the full selected/custom state.
        if (expected.draft.picks.mapValues { it.value.toList().sorted() } != actual.picks.mapValues { it.value.toList().sorted() }) {
            report("picks", shape)
        }
        // Exact values stay in this worker. Only a shape code is ever reported, never a custom answer's text.
        if (expected.draft.other != actual.other) report("other-state", shape)
        val expectedAnswers = expected.resolution?.answers
        val actualAnswers = actual.resolution?.answers
        if ((expectedAnswers == null) != (actualAnswers == null)) {
            report("resolution-presence", shape)
        } else if (expectedAnswers != null && actualAnswers != null && expectedAnswers != actualAnswers) {
            // Exact comparison happens here in memory. Only a shape code leaves this worker.
            report("answers", shape)
        }
        if (expected.resolution?.copy(answers = null) != actual.resolution?.copy(answers = null)) {
            report("resolution-fields", shape)
        }
    }

    private fun completed() {
        ShadowAnswerDiagnostics.compared()
        val count = compared.incrementAndGet()
        if (count % 10 == 0) {
            try { progress(count, mismatchCount) } catch (_: Throwable) {}
        }
    }

    private fun report(rule: String, shape: String) {
        val report = AnswerMismatch(domain = "question-answer", rule = rule, inputShape = shape)
        try { sink?.invoke(report) } catch (_: Throwable) {}
        mismatches.incrementAndGet()
        try { ShadowAnswerDiagnostics.report(report) } catch (_: Throwable) {}
    }
}

@Serializable
internal data class CoreTransition(
    val allowed: Boolean,
    val page: Int,
    val picks: Map<String, Set<String>> = emptyMap(),
    val other: Map<String, String> = emptyMap(),
    val resolution: Resolution? = null,
)

internal fun encodeBuild(interaction: Interaction, action: AnswerAction, draft: AnswerDraft, context: AnswerContext): JsonObject = buildJsonObject {
    put("interaction", buildJsonObject {
        put("kind", interaction.kind)
        put("questions", JsonArray(interaction.questions.orEmpty().map { it.coreJson() }))
    })
    put("operation", buildJsonObject {
        put("action", action.wire)
        put("page", draft.page)
        put("picks", JsonObject(draft.picks.mapValues { (_, labels) -> JsonArray(labels.sorted().map(::JsonPrimitive)) }))
        put("other", JsonObject(draft.other.mapValues { (_, text) -> JsonPrimitive(text) }))
        put("sending", context.sending)
        if (action == AnswerAction.PickOption) put("option_index", draft.optionIndex)
    })
}

private fun Question.coreJson(): JsonObject = buildJsonObject {
    put("question", question)
    put("multi_select", multiSelect)
    put("options", JsonArray(options.map { buildJsonObject { put("label", it.label) } }))
}

private fun shape(interaction: Interaction, action: AnswerAction, draft: AnswerDraft, context: AnswerContext): String {
    val questions = interaction.questions.orEmpty()
    return listOf(
        "questions=${questions.size}",
        "options=${questions.sumOf { it.options.size }}",
        "multiSelect=${questions.count { it.multiSelect }}",
        "picked=${draft.picks.values.sumOf { it.size }}",
        "other=${draft.other.size}",
        "action=${action.wire}",
        "entry=${context.entry.wire}",
        "pageInRange=${draft.page in questions.indices}",
        "sending=${context.sending}",
    ).joinToString(" ")
}
