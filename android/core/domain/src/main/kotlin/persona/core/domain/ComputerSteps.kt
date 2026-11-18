package persona.core.domain

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import persona.core.contracts.CoreText
import persona.core.contracts.CoreText.Key
import persona.core.contracts.GroupKeys
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.contracts.parseTime

/**
 * What a tool that operates a Mac does, in words (iOS `ComputerSteps`): the assistant's computer_run / computer_state
 * and an agent's own Cua Driver MCP tools (mcp__cua-driver__click…), docs/extensions/computer-use.md. "Click AC → 6 →
 * × → 7 → =" rather than its JSON.
 */
object ComputerSteps {
    /** A tool operates a Mac: the assistant's, or Cua Driver's own MCP. */
    fun isComputerTool(name: String): Boolean {
        val base = toolBaseName(name)
        return base == "computer_run" || base == "computer_state" || isCuaTool(name)
    }

    fun isCuaTool(name: String): Boolean {
        val lower = name.lowercase()
        return lower.startsWith("mcp__cua") || "cua-driver" in lower || "cua_driver" in lower
    }

    /** The whole call in a line: "Click AC → 6 → × → 7 → =", "Type “hello” → Press ⏎", "Look at Calculator", "Run script: …". */
    fun describe(name: String, input: JsonObject?): CoreText {
        val i = input ?: JsonObject(emptyMap())
        return when (val base = toolBaseName(name)) {
            "computer_state" -> i.text("app")?.takeIf(String::isNotEmpty)?.let { CoreText.of(Key.ComputerLookAt, it) }
                ?: CoreText.of(Key.ComputerLookAtScreen)
            "computer_run" -> run(i) ?: CoreText.of(Key.ToolComputerRun)
            else -> {
                // Cua Driver's own tool: its name is the action, its input the target.
                val tool = base.replace('-', '_')
                val steps = i["steps"] as? JsonArray
                when {
                    tool == "run_steps" && steps != null -> join(steps.mapNotNull(::step))
                    tool == "run_script" -> CoreText.of(Key.ComputerScript)
                    else -> action(tool, i)?.text ?: CoreText.plain(tool)
                }
            }
        }
    }

    /**
     * A computer_run row's subject when the call says what it does (steps or a script): "MacBook · Click AC → 6 → ×";
     * null for any other call, which keeps the row's own subject (its machine alone). [machine] names the Mac the call's
     * `machine` is ([machineName]).
     */
    fun runSubject(name: String, input: JsonObject?, machine: (String) -> String = { named -> named }): CoreText? {
        if (toolBaseName(name) != "computer_run" || input == null) return null
        val what = run(input) ?: return null
        val named = input.text("machine")?.takeIf(String::isNotBlank)
        return CoreText.joined(" · ", listOfNotNull(named?.let { CoreText.plain(machine(it)) }, what))
    }

    /**
     * The Mac a tool's `machine` names, as the user knows it ("MacBook", not "macbook"; iOS `machineName`): matched by id
     * or by name as the hub does, in the chat's own hub group ([chatMachine]'s) first and then in any; what the tool said
     * when no machine goes by it.
     */
    fun machineName(named: String, machines: List<Machine>, chatMachine: String = ""): String {
        fun match(m: Machine) = GroupKeys.raw(m.id) == named || m.name.equals(named, ignoreCase = true)
        val group = GroupKeys.namespaceOf(chatMachine)
        val found = machines.firstOrNull { GroupKeys.namespaceOf(it.id) == group && match(it) } ?: machines.firstOrNull(::match)
        return found?.name?.takeIf(String::isNotEmpty) ?: named
    }

    /** A computer_run's steps, else its script's first line; null when it has neither. */
    private fun run(input: JsonObject): CoreText? {
        val steps = input["steps"] as? JsonArray
        if (!steps.isNullOrEmpty()) return join(steps.mapNotNull(::step))
        val script = input.text("script") ?: return null
        val first = firstLine(script.trim())
        return if (first.isEmpty()) CoreText.of(Key.ComputerScript) else CoreText.of(Key.ComputerScriptNamed, first)
    }

    /** One step in words, and for a click what it hit, so that a run of clicks names the verb once ([join]). */
    private class Step(val text: CoreText, val clicked: CoreText? = null)

    /** Steps in a line, clicks after a click joined by → alone (Click AC → 6 → ×). */
    private fun join(steps: List<Step>): CoreText {
        var clicking = false
        return CoreText.joined(" → ", steps.map { step ->
            (if (step.clicked != null && clicking) step.clicked else step.text).also { clicking = step.clicked != null }
        })
    }

    /** One run_steps step: {"click": {…}} or {"tool": "click", "args": {…}}; a check alone is "Check". */
    private fun step(value: JsonElement): Step? {
        val o = value as? JsonObject ?: return null
        o.text("tool")?.let { return action(it, o["args"] as? JsonObject ?: JsonObject(emptyMap())) }
        o.entries.firstOrNull { it.key !in setOf("wait_for", "expect", "args", "tool") }?.let { (key, args) ->
            return action(key, args as? JsonObject ?: JsonObject(emptyMap()))
        }
        return if (o["expect"] != null || o["wait_for"] != null) Step(CoreText.of(Key.ComputerCheck)) else null
    }

    /** One action: its verb and what it acts on; null for an action this does not know. */
    private fun action(tool: String, args: JsonObject): Step? {
        val target = targetName(args)?.let(CoreText::plain) ?: CoreText.of(Key.ComputerSpot)
        fun of(key: Key, vararg values: Any) = Step(CoreText.of(key, *values))
        return when (tool) {
            "click" -> Step(CoreText.of(Key.ComputerClick, target), clicked = target)
            "double_click" -> of(Key.ComputerDoubleClick, target)
            "right_click" -> of(Key.ComputerRightClick, target)
            "type_text" -> of(Key.ComputerType, short(args.text("text").orEmpty()))
            "set_value" -> of(Key.ComputerFillIn, short(args.text("value").orEmpty()))
            "press_key" -> of(Key.ComputerPress, key(args.text("key").orEmpty()))
            "hotkey" -> of(Key.ComputerPress, CoreText.joined("", (args["keys"] as? JsonArray).orEmpty()
                .mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.map(::key)))
            "scroll" -> of(Key.ComputerScroll)
            "drag" -> of(Key.ComputerDrag)
            "launch_app" -> of(Key.ComputerOpen, args.text("name") ?: args.text("bundle_id") ?: "App")
            "bring_to_front" -> (args.text("app") ?: args.text("name"))?.let { of(Key.ComputerSwitchTo, it) } ?: of(Key.ComputerBringToFront)
            "get_window_state" -> of(Key.ComputerLookAtWindow)
            "get_desktop_state", "screenshot" -> of(Key.ComputerScreenshot)
            "get_accessibility_tree", "list_windows", "list_apps" -> of(Key.ComputerSeeScreen)
            "zoom" -> of(Key.ComputerZoom)
            else -> null
        }
    }

    /** What a step's target is called: a button's name (Calculator's are words: Multiply → ×), else none. */
    private fun targetName(args: JsonObject): String? {
        val name = (args.text("name") ?: args.text("label") ?: args.text("text"))?.takeIf(String::isNotEmpty) ?: return null
        return symbols[name] ?: short(name)
    }

    /** Calculator's buttons read as on its keys. */
    private val symbols = mapOf(
        "All Clear" to "AC", "Clear" to "C", "Multiply" to "×", "Divide" to "÷", "Add" to "+", "Plus" to "+", "Subtract" to "−",
        "Minus" to "−", "Equals" to "=", "Percent" to "%", "Decimal" to ".", "Change Sign" to "±", "Negate" to "±",
    )

    /** A key as on a Mac's keyboard (⌘, ⏎); a letter in capitals. */
    fun key(k: String): CoreText = when (k.lowercase()) {
        "cmd", "command" -> CoreText.plain("⌘")
        "shift" -> CoreText.plain("⇧")
        "option", "alt" -> CoreText.plain("⌥")
        "ctrl", "control" -> CoreText.plain("⌃")
        "return", "enter" -> CoreText.plain("⏎")
        "escape", "esc" -> CoreText.plain("esc")
        "tab" -> CoreText.plain("⇥")
        "space" -> CoreText.of(Key.ComputerSpace)
        "delete", "backspace" -> CoreText.plain("⌫")
        else -> CoreText.plain(if (k.length == 1) k.uppercase() else k)
    }

    private fun firstLine(s: String): String = s.lineSequence().firstOrNull(String::isNotEmpty) ?: s

    /** The first line, cut to 23 characters and "…" past 24. */
    fun short(s: String): String = firstLine(s).let { if (it.length > 24) it.take(23) + "…" else it }

    private fun JsonObject.text(key: String): String? = (get(key) as? JsonPrimitive)?.contentOrNull
}

/**
 * The latest time a chat's model operated a Mac (iOS `ComputerActivity`): which Mac, the step in words, still running
 * or when it ended. The chat shows it as a pill above the composer while it runs and for [LINGERS_MS] after.
 */
data class ComputerActivity(
    /** The Mac's id as the app's lists have it (another hub group's with its suffix, [GroupKeys]). */
    val machineId: String,
    val machineName: String,
    val step: CoreText,
    val running: Boolean,
    /** When it ended (its result's time); null while it runs, or when the result has no time. */
    val endedAtMs: Long?,
    /** The tool call. */
    val call: Item,
) {
    val toolUseId: String get() = call.toolUseId.orEmpty()

    /** Showing at [nowMs]: running, or ended a moment ago. */
    fun shown(nowMs: Long): Boolean = running || (endedAtMs != null && nowMs - endedAtMs < LINGERS_MS)

    /** "Controlling MacBook · Click AC → 6", "Just controlled MacBook · …" once it ended. */
    val title: CoreText get() = CoreText.of(if (running) Key.ComputerControlling else Key.ComputerControlled, machineName, step)

    /** [title] for a screen reader, with a comma for the dot. */
    val spoken: CoreText get() = CoreText.of(if (running) Key.ComputerControllingSpoken else Key.ComputerControlledSpoken, machineName, step)

    companion object {
        /** How long a finished one stays up before it fades. */
        const val LINGERS_MS = 6_000L

        /**
         * The chat's latest computer tool call among the newest [window] of [items] (oldest first), in the chat of
         * [chatMachine]; null when there is none or the Mac it names is not one of [machines].
         */
        fun latest(items: List<Item>, chatMachine: String, machines: List<Machine>, window: Int = 300): ComputerActivity? {
            val resultAt = mutableMapOf<String, Long?>()
            for (item in items.takeLast(window).asReversed()) {
                if (item.kind == "tool_result") item.toolUseId?.let { resultAt[it] = parseTime(item.ts)?.toEpochMilli() }
                val name = item.toolName
                if (item.kind != "tool_use" || name == null || item.toolUseId == null || !ComputerSteps.isComputerTool(name)) continue
                val input = item.toolInput as? JsonObject
                val machine = machine(name, input, chatMachine, machines) ?: return null
                val ended = item.toolUseId in resultAt
                return ComputerActivity(machine, machines.firstOrNull { it.id == machine }?.name?.takeIf(String::isNotEmpty) ?: GroupKeys.raw(machine),
                    ComputerSteps.describe(name, input), running = !ended, endedAtMs = resultAt[item.toolUseId], call = item)
            }
            return null
        }

        /**
         * The Mac a tool call operates: the one computer_* names (by id or name, in the chat's group of hubs), or the
         * chat's own for an agent's Cua Driver tools (iOS `ScreenEntry.machine`).
         */
        fun machine(name: String, input: JsonObject?, chatMachine: String, machines: List<Machine>): String? {
            val base = toolBaseName(name)
            if (base == "computer_state" || base == "computer_run") {
                val named = (input?.get("machine") as? JsonPrimitive)?.contentOrNull?.takeIf(String::isNotEmpty) ?: return null
                val group = GroupKeys.namespaceOf(chatMachine)
                return machines.firstOrNull {
                    GroupKeys.namespaceOf(it.id) == group && (GroupKeys.raw(it.id) == named || it.name.equals(named, ignoreCase = true))
                }?.id
            }
            return chatMachine.takeIf { ComputerSteps.isCuaTool(name) }
        }
    }
}
