package persona.core.domain.inbox

/**
 * The Inbox's section bar (#9053, docs/assistant/inbox.md "Section bar"; the shared cases are spec/fixtures/inbox/bar.json,
 * bar_landing.json and bar_search.json): the choices it offers with their counts, the sections a choice draws, what a
 * choice with nothing to draw says, where an item opened from outside the list lands, and how a search moves the choice.
 * It reads [buildInboxList]'s groups, so the counts follow the source, kind and search as the list does.
 */
object InboxBar {
    const val ACTION = "action"
    const val WORKING = InboxItem.WORKING
    const val FYI = "fyi"
    const val NOISE = "noise"
    const val DONE = "done"
    const val ALL = "all"

    /** The choices in the bar's order; Collapsed ([NOISE]) has none of its own and shows under [ALL]. */
    val CHOICES = listOf(ACTION, WORKING, FYI, DONE, ALL)

    /** The open sections in the list's order. */
    private val OPEN = listOf(ACTION, WORKING, FYI, NOISE)

    /** A count's look: Needs you's, for what waits for the user, or the neutral one of a passive total. */
    enum class Tone { Needs, Neutral }

    /** One choice: its id, how many rows it holds (null: it shows no number) and the look of that number. */
    data class Choice(val id: String, val count: Int?, val tone: Tone?)

    /**
     * What a choice with nothing to draw says: [CaughtUp] only when nothing is open anywhere, [Section] in the chosen
     * section's own words while other sections hold rows, [Filtered] and [NoMatch] when a source or kind, or a search,
     * hides everything.
     */
    enum class Empty { CaughtUp, Section, Filtered, NoMatch }

    /** The bar's [choices], the sections drawn ([shown], in the list's order) and what the list says when it has none to draw. */
    data class Bar(val choices: List<Choice>, val shown: List<String>, val empty: Empty?)

    /** A choice the bar knows, else Needs you (a stored or linked value from another version). */
    fun choice(id: String): String = id.takeIf { it in CHOICES } ?: ACTION

    /**
     * The bar over [groups] ([buildInboxList]'s, under [settings]' source, kind and search) with [chosen] picked.
     * [doneDays] is the hub's done items per day added up (null: a hub that doesn't count them): with no filter or
     * search on it keeps Done in the bar while the list itself holds no done item.
     */
    fun bar(groups: List<InboxGroup>, chosen: String, settings: InboxListSettings, doneDays: Int? = null): Bar {
        val picked = choice(chosen)
        val rows = groups.associate { it.id to it.count }
        fun count(id: String) = rows[id] ?: 0
        val narrowed = settings.source.isNotEmpty() || settings.kind.isNotEmpty()
        val searching = settings.query.isNotEmpty()
        val done = count(DONE) > 0 || (!narrowed && !searching && (doneDays ?: 0) > 0)
        val choices = buildList {
            add(Choice(ACTION, count(ACTION), Tone.Needs))
            if (count(WORKING) > 0 || picked == WORKING) add(Choice(WORKING, count(WORKING), Tone.Neutral))
            if (count(FYI) > 0 || picked == FYI) add(Choice(FYI, count(FYI), Tone.Neutral))
            if (done || picked == DONE) add(Choice(DONE, null, null))
            add(Choice(ALL, null, null))
        }
        // Done on the hub's counts alone draws only where it is chosen; All stays the stacked page, every section with rows.
        fun present(id: String) = if (id == DONE && picked == DONE) done else count(id) > 0
        val shown = (if (picked == ALL) OPEN + DONE else listOf(picked)).filter(::present)
        val empty = when {
            picked == DONE -> when {
                done -> null
                narrowed -> Empty.Filtered
                searching -> Empty.NoMatch
                else -> Empty.Section
            }
            (if (picked == ALL) OPEN else listOf(picked)).any { count(it) > 0 } -> null
            OPEN.any { count(it) > 0 } -> Empty.Section
            narrowed -> Empty.Filtered
            searching -> Empty.NoMatch
            else -> Empty.CaughtUp
        }
        return Bar(choices, shown, empty)
    }

    /** Where the list goes for an item opened from outside it: its section, or All with Collapsed open. */
    data class Landing(val choice: String, val openCollapsed: Boolean)

    /**
     * [item]'s [Landing]: Done for a done item, All with Collapsed open for a collapsed one, else its section.
     * [working]: Persona has it in hand by an older hub's word (the AI twin drafting), which the list shows In progress too.
     */
    fun landing(item: InboxItem, working: Boolean = false): Landing = when {
        item.isDone -> Landing(DONE, false)
        working -> Landing(WORKING, false)
        item.section == NOISE -> Landing(ALL, true)
        else -> Landing(choice(item.section), false)
    }

    /** The choice shown ([chosen]) and the one a search set aside ([saved]), which clearing the search brings back. */
    data class State(val chosen: String, val saved: String? = null)

    /** Entering the Inbox: Needs you, or All with Needs you set aside when a kept search text comes with it. */
    fun entered(query: String): State = if (query.isEmpty()) State(ACTION) else State(ALL, ACTION)

    /** The search text went from [before] to [after]: text appearing sets the choice aside and shows All; cleared, it comes back. */
    fun searched(state: State, before: String, after: String): State = when {
        before.isEmpty() && after.isNotEmpty() -> State(ALL, state.chosen)
        before.isNotEmpty() && after.isEmpty() -> State(state.saved ?: ACTION)
        else -> state
    }

    /** A choice tapped: it shows, and during a search it narrows the matches; what the search set aside stays. */
    fun picked(state: State, choice: String): State = state.copy(chosen = choice(choice))

    /**
     * An item opened from outside the list: the list moves to where it is ([landing]). A list that already shows it (All,
     * or the item's own choice) stays; Collapsed opening under All is the caller's ([Landing.openCollapsed]).
     */
    fun landed(state: State, landing: Landing): State =
        if (state.chosen == ALL || state.chosen == landing.choice) state else state.copy(chosen = landing.choice)
}
