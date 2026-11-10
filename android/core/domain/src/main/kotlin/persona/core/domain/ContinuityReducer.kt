package persona.core.domain

import persona.core.contracts.Item

/**
 * iOS continuity rules from spec/fixtures/continuity and ChatStore.apply/merge/replace.
 *
 * [covered] starts at zero (not -1). A push continuing from zero establishes a baseline;
 * a later push without a baseline waits for backlog, then requests reload if [loadedOnce].
 * Cache restoration does not count as backlog. Only backlog sets [loadedOnce]; [replace]
 * deliberately preserves it. A changed history clears items, coverage and pagination before
 * accepting a self-contained page or requesting reload for a continuing batch.
 *
 * Ordinary duplicate IDs keep their first value; changed folds replace the work they cover.
 * Callers serialize access and execute effects outside this reducer (see [ContinuitySync]).
 */
class ContinuityReducer {
    enum class Gap { None, Fill, Reload }

    data class PushResult(
        val reloadLatest: Boolean = false,
        val fillGap: Boolean = false,
        val moreAfter: Boolean = false,
    )

    private val byId = LinkedHashMap<String, Item>()
    private var ordered: List<Item>? = null
    var history: String? = null
        private set
    var covered: Int = 0
        private set
    /** Null means no accepted pagination information, including after a history reset. */
    var hasMore: Boolean? = null
        private set
    var loadedOnce: Boolean = false
        private set
    val firstSeq: Int? get() = byId.values.minOfOrNull { it.seq }
    val items: List<Item>
        get() = ordered ?: byId.values.sortedWith(compareBy<Item> { it.seq }.thenBy { part(it.id) }).also { ordered = it }

    /** Restores cached state without treating the cache as a received backlog. */
    fun restore(items: List<Item>, history: String?, covered: Int, hasMore: Boolean? = null, loadedOnce: Boolean = false) {
        byId.clear()
        ordered = null
        items.forEach { byId.putIfAbsent(it.id, it) }
        this.history = history
        this.covered = covered.coerceAtLeast(0)
        this.hasMore = hasMore
        this.loadedOnce = loadedOnce
    }

    /**
     * Initial backlog: merges adjacent/overlapping pages, replaces distant cache, marks loaded.
     * Reload callers must use [replace] to discard a hole while retaining overtaking pushes.
     */
    fun latest(items: List<Item>, history: String?, toSeq: Int?, hasMore: Boolean? = null) {
        apply(items, history, toSeq = toSeq, backlog = true, hasMore = hasMore)
    }

    /**
     * Source-compatible push adapter. Pass backlog and nullable hasMore from the envelope.
     * moreAfter is actionable only when the resulting gap is Fill; before the first backlog
     * it cannot bypass the baseline gate, and a legacy capped batch requests reload instead.
     */
    fun push(items: List<Item>, history: String?, fromSeq: Int?, toSeq: Int?, moreAfter: Boolean,
             backlog: Boolean = false, hasMore: Boolean? = null): PushResult {
        val gap = apply(items, history, fromSeq, toSeq, backlog, hasMore, moreAfter)
        return PushResult(gap == Gap.Reload, gap == Gap.Fill, moreAfter && gap == Gap.Fill)
    }

    /**
     * Source-compatible catch-up adapter; omitted fromSeq assumes a request after current covered.
     * False means reload. New callers should use [apply] to retain the full gap decision and
     * pass the response's actual fromSeq, including null for legacy hubs.
     */
    fun newer(items: List<Item>, history: String?, toSeq: Int?, fromSeq: Int? = covered,
              moreAfter: Boolean = false): Boolean =
        apply(items, history, fromSeq, toSeq, moreAfter = moreAfter) != Gap.Reload

    fun apply(items: List<Item>, history: String? = null, fromSeq: Int? = null, toSeq: Int? = null,
              backlog: Boolean = false, hasMore: Boolean? = null, moreAfter: Boolean = false): Gap {
        if (history != null && this.history != null && history != this.history) {
            restore(emptyList(), history, 0)
            if (fromSeq != null && fromSeq > 0) return Gap.Reload
        }
        if (history != null) this.history = history
        if (toSeq == null) {
            merge(items, backlog, hasMore)
            covered = maxOf(covered, byId.values.maxOfOrNull { it.seq } ?: 0)
            return if (moreAfter) Gap.Reload else Gap.None
        }
        if (fromSeq != null) {
            merge(items, backlog, hasMore)
            if (covered == 0 && fromSeq > 0) return if (loadedOnce) Gap.Reload else Gap.None
            if (fromSeq > covered) return Gap.Fill
            covered = maxOf(covered, toSeq)
            return if (moreAfter) Gap.Fill else Gap.None
        }
        if (covered > 0 && items.firstOrNull()?.seq?.let { it.toLong() > covered.toLong() + 1 } == true) {
            replace(items, history, toSeq, hasMore ?: true)
            if (backlog) loadedOnce = true
            return Gap.None
        }
        merge(items, backlog, hasMore)
        covered = maxOf(covered, toSeq)
        return Gap.None
    }

    /**
     * Reload completion keeps same-history pushes strictly past toSeq, but does not claim
     * coverage through them. Unlike [latest], this does not change [loadedOnce].
     */
    fun replace(items: List<Item>, history: String?, toSeq: Int?, hasMore: Boolean) {
        val keep = if (history == null || history == this.history)
            byId.values.filter { toSeq != null && it.seq > toSeq } else emptyList()
        byId.clear()
        ordered = null
        this.hasMore = hasMore
        if (history != null) this.history = history
        merge(items)
        merge(keep)
        covered = toSeq ?: (byId.values.maxOfOrNull { it.seq } ?: 0)
    }

    /** Older pages set pagination directly without changing history, coverage or loadedOnce. */
    fun older(items: List<Item>, hasMore: Boolean? = null) {
        if (hasMore != null) this.hasMore = hasMore
        merge(items)
    }

    /** Publish accumulated range pages together, discarding fold placeholders in the response. */
    fun unfold(fold: Item, work: List<Item>) {
        if (byId.remove(fold.id) == null) return
        ordered = null
        merge(work.filter { it.kind != "fold" })
    }

    private fun merge(new: List<Item>, backlog: Boolean = false, hasMore: Boolean? = null) {
        val folds = new.filter { it.kind == "fold" && byId[it.id]?.let { old -> old.fold == it.fold } != true }
        if (folds.isNotEmpty()) {
            if (byId.values.removeAll { it.isWork() && folds.any { fold -> fold.folds(it) } }) ordered = null
        }
        // iOS checks the first existing item before sorting the newly appended older items.
        val existingFirst = firstSeq
        new.forEach { if (byId.putIfAbsent(it.id, it) == null) ordered = null }
        val reachesStart = new.minOfOrNull { it.seq }?.let { it <= (existingFirst ?: new.first().seq) } ?: false
        if (hasMore != null && backlog && (!loadedOnce || byId.size <= new.size) && (hasMore || reachesStart)) {
            this.hasMore = hasMore
        }
        if (backlog) loadedOnce = true
    }

    private fun Item.isWork(): Boolean = when (kind) {
        "thinking", "tool_use", "tool_result", "fold" -> true
        "image" -> toolUseId != null
        else -> false
    }

    private fun Item.folds(other: Item): Boolean {
        val range = fold ?: return false
        return (other.seq > seq || other.seq == seq && part(other.id) >= part(id)) &&
            (other.seq < range.throughSeq || other.seq == range.throughSeq && part(other.id) <= part(range.throughId))
    }

    private fun part(id: String): Int = id.substringAfterLast('#', "").toIntOrNull() ?: 0
}
