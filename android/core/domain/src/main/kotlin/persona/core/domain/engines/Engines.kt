package persona.core.domain.engines

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.DetectedEngine
import persona.core.contracts.Machine

/**
 * One terminal engine per machine (docs/integrations/engines.md), as on iOS `EngineSelectionModel`. The options are what the hub
 * reports in `engines`; [known] only names the engines this build knows: for hubs that report nothing, and to decide
 * what may be chosen without detection (an offline machine).
 */
object Engines {
    const val DEFAULT = "herdr"

    /** An engine this build knows: its id, a fallback name, and where its sessions live. */
    data class Known(val id: String, val name: String, val detail: CoreText)

    val known = listOf(
        Known("herdr", "herdr", CoreText.of(Key.EngineHerdr)),
        Known("herdr-core", "Herdr App", CoreText.of(Key.EngineHerdrApp)),
        Known("orca", "Orca", CoreText.of(Key.EngineOrca)),
        Known("paseo", "Paseo", CoreText.of(Key.EnginePaseo)),
        Known("cindy", "Cindy", CoreText.of(Key.EngineCindy)),
    )

    fun detail(id: String): CoreText? = known.firstOrNull { it.id == id }?.detail

    /** The chosen engine; herdr when the hub sent none. */
    fun current(machine: Machine): String = machine.engine.ifBlank { DEFAULT }

    /** The chosen engine's name: the hub's name for it, else the known one, else its id. */
    fun currentName(machine: Machine): String = name(current(machine), machine)

    fun name(id: String, machine: Machine): String =
        machine.engines.firstOrNull { it.id == id }?.name?.takeIf { it.isNotBlank() } ?: known.firstOrNull { it.id == id }?.name ?: id

    /** The machine pairing asks about: the hub's own (kind local), else the first one. */
    fun pairingMachine(machines: List<Machine>): Machine? = machines.firstOrNull { it.kind == "local" } ?: machines.firstOrNull()

    /** Pairing asks only when the hub reports engines: an older hub cannot save a choice and the step must not trap. */
    fun pairingStep(machines: List<Machine>): Machine? = pairingMachine(machines)?.takeIf { it.engines.isNotEmpty() }

    /**
     * What the picker starts on: the machine's current engine, herdr when never chosen. Never guessed from what happens
     * to be detected (2026-11-01, the user: an engine must be picked, herdr by default).
     */
    fun initialChoice(machine: Machine): String = current(machine)

    /** The rows to list: the hub's `engines` as given; with none reported, the known engines (not detected). */
    fun options(machine: Machine): List<DetectedEngine> =
        machine.engines.ifEmpty { known.map { DetectedEngine(it.id, it.name, available = false) } }

    /** Known engines can be set without detection (offline: applied on connect); an unknown one only when found there. */
    fun canSelect(id: String, machine: Machine): Boolean =
        known.any { it.id == id } || machine.engines.any { it.id == id && it.available }

    /** The detection line under an option; missing detection is not reported as "Not detected". */
    fun detection(id: String, machine: Machine): CoreText {
        if (!machine.online) return CoreText.of(Key.EngineOffline)
        val found = machine.engines.firstOrNull { it.id == id } ?: return CoreText.of(Key.EngineUnknown)
        return when {
            !found.available -> CoreText.of(Key.EngineMissing)
            !found.version.isNullOrBlank() -> CoreText.of(Key.EngineFoundVersion, found.version.orEmpty())
            else -> CoreText.of(Key.EngineFound)
        }
    }

    /** The note under the options. */
    fun footer(machine: Machine): CoreText = CoreText.of(if (machine.online) Key.EngineFooter else Key.EngineFooterOffline)

    /** What "Save" says once the hub took the choice. */
    fun saved(machine: Machine): CoreText = CoreText.of(if (machine.online) Key.EngineSwitched else Key.EngineSavedOffline)
}
