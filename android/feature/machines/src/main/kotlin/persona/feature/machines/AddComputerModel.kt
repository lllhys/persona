package persona.feature.machines

import androidx.annotation.StringRes
import persona.core.contracts.GroupKeys
import persona.core.contracts.Machine
import persona.core.contracts.api.ComputerCode
import persona.core.contracts.api.ComputerOs
import persona.core.contracts.api.ComputerPairing
import persona.core.contracts.api.CurrentAssistant
import persona.core.contracts.api.HubManager
import persona.core.contracts.api.ManagerRoster
import persona.core.contracts.api.hubCodedFailure
import persona.core.designsystem.UiText
import persona.core.designsystem.failureReasonText
import persona.core.designsystem.hubMessage
import persona.core.designsystem.shownName
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

// Machines › Add a computer (docs/assistant/onboarding.md 4.2 "Your computer <-> hub", after the web's AddComputer): pick the system, name it, and the
// hub adds the computer and gives its Persona Link a pairing link (POST /api/v1/auth/pair-codes {"add"}, feature
// machines_add). The page then says how to use the link on that system, waits for the computer to come online and asks,
// on the same screen, whether it is yours (…/confirm | …/reject). Opened to confirm one, it starts at that question.

/** Where adding a computer is, from what the machine list says of it now. */
internal enum class AddStage {
    /** The name and the system (the code being made, or why the hub refused, with them). */
    Form,
    Waiting, Expired,
    /** Online and waiting to be confirmed: "It's mine" / "No, remove". */
    Joined,
    /** Confirmed (here or elsewhere). */
    Connected,
    /** Rejected here, or removed elsewhere (rejected, or it waited too long). */
    Gone,
    /** Confirming one with no code made here, or one that joined since the code was made: it waits but is not online to be looked at. */
    Offline,
}

internal enum class AddOutcome { Confirmed, Rejected }

internal data class AddComputerState(
    val os: ComputerOs = ComputerOs.Mac,
    val name: String = "",
    val code: ComputerCode? = null,
    /** The computer being confirmed (opened from "Confirm computer" or a pending machine's row), when no code was made here. */
    val confirming: String? = null,
    /** Opened as "Confirm computer": never the add form, even while no waiting computer has been found yet. */
    val confirmOnly: Boolean = false,
    val busy: Boolean = false,
    val answering: AddOutcome? = null,
    val outcome: AddOutcome? = null,
    val error: UiText? = null,
    val now: Long = 0,
    /** The machine list read while waiting (the WebSocket's list may not have it yet). */
    val polled: List<Machine>? = null,
    /** The list has had it since the code was made: when it is gone after that, it was removed. */
    val seen: Boolean = false,
    /** It has been online since the code was made: offline after that, it is asleep, not waiting for the link (#5307). */
    val joined: Boolean = false,
    /** The manager the new computer was asked for (`add.manager`); null when none was named (no roster). */
    val asked: String? = null,
    /** The roster when it was asked, to name the assistant the hub put it under. */
    val roster: ManagerRoster? = null,
    /** The hub refused a new one while 3 wait (`too_many_pending`): the computers in the way are listed, each with Remove. */
    val tooMany: Boolean = false,
    /** The computer in the way being removed. */
    val removing: String? = null,
) {
    val machineId: String? get() = code?.machine ?: confirming
    val secondsLeft: Int get() = code?.let { c -> if (c.expiresAt <= 0) 0 else maxOf(0L, (c.expiresAt - now + 999) / 1000).toInt() } ?: 0
    /** The display name: the hub's, the list's, or what was typed. */
    fun shown(machine: Machine?): String = code?.name?.takeIf { it.isNotBlank() } ?: machine?.displayName ?: confirming ?: name.trim()
}

/**
 * The assistant the new computer is under when that is not the one it was asked for (docs/protocol/protocol.md "Computer
 * pairing codes"): the hub's answer names another, or names none (a hub from before `add.manager` puts a new computer under
 * its own assistant). Null when it is where it was asked for, when none was asked for, and with one assistant.
 */
internal fun AddComputerState.placedUnder(listed: List<Machine> = emptyList(), current: ManagerRoster? = null): String? {
    val code = code ?: return null
    val asked = asked ?: return null
    val roster = (current ?: roster)?.takeIf { it.isMulti } ?: return null
    // The reply's manager says it; a hub from before add.manager leaves it out and puts a computer it added under its own
    // assistant, while one it reused stays where it was (the machines list has it under that one).
    val owner = code.manager?.takeIf { it.isNotBlank() }
        ?: if (code.added) ManagerRoster.ASSISTANT_MANAGER
        else listed.firstOrNull { it.id == code.machine }?.manager?.takeIf { it.isNotBlank() } ?: ManagerRoster.ASSISTANT_MANAGER
    if (owner == asked) return null
    return roster.manager(owner)?.shownName ?: owner
}

/** The current assistant when a computer can't be added to it: it is offline (with several; one is never told apart). */
internal fun CurrentAssistant.offlineForAdding(): HubManager? =
    manager?.takeIf { roster?.isMulti == true && !it.online && !it.isInstalling }

/** The computers keeping a new one from being added: every waiting one, whichever assistant's (the hub's limit is the hub's). */
internal fun inTheWay(machines: List<Machine>, group: String? = null): List<Machine> =
    machines.filter { it.pending && GroupKeys.namespaceOf(it.id) == group }

/** The machine as the WebSocket's list or the polled one has it, whichever has it online first. */
internal fun AddComputerState.machine(listed: List<Machine>): Machine? {
    val id = machineId ?: return null
    val fromList = listed.firstOrNull { it.id == id }
    val fromPoll = polled?.firstOrNull { it.id == id }
    return listOf(fromList, fromPoll).firstOrNull { it?.online == true } ?: fromList ?: fromPoll
}

internal fun AddComputerState.stage(listed: List<Machine>): AddStage {
    if (outcome == AddOutcome.Confirmed) return AddStage.Connected
    if (outcome == AddOutcome.Rejected) return AddStage.Gone
    val m = machine(listed)
    if (code == null) {
        if (confirming == null) return if (confirmOnly) AddStage.Gone else AddStage.Form
        return when {
            m == null -> AddStage.Gone
            !m.pending -> AddStage.Connected
            m.online -> AddStage.Joined
            else -> AddStage.Offline
        }
    }
    if (m == null && seen) return AddStage.Gone
    // Joined, or it has a hub error other than never having connected: a new code would revoke a pairing that works.
    if (m != null && !m.online && (joined || (m.errorCode != null && m.errorCode != "never_connected"))) return AddStage.Offline
    if (m == null || !m.online) return if (secondsLeft > 0) AddStage.Waiting else AddStage.Expired
    return if (m.pending) AddStage.Joined else AddStage.Connected
}

/** The computer "Confirm computer" opens: [preferred] while it waits, else the first waiting one online, else the first waiting. */
internal fun pendingToConfirm(machines: List<Machine>, preferred: String?): Machine? {
    val waiting = machines.filter { it.pending }
    return waiting.firstOrNull { preferred != null && it.id == preferred } ?: waiting.firstOrNull { it.online } ?: waiting.firstOrNull()
}

/** A machine's error_code in words; null for a code the app does not know (its error is shown instead). */
@StringRes
internal fun machineErrorText(code: String?): Int? = when (code) {
    "node_service_down" -> R.string.machines_onb_code_node_service_down
    "link_offline" -> R.string.machines_onb_code_link_offline
    "never_connected" -> R.string.machines_onb_code_never_connected
    "node_outdated" -> R.string.machines_onb_code_node_outdated
    else -> null
}

/** Why the hub made no code for a computer, by its code; the hub's own words for any other. */
internal fun addErrorText(error: Throwable): UiText = when (hubCodedFailure(error)?.code) {
    // The assistant asked for (gone, still installing, its computers' owners not known yet): the hub's words.
    "no_manager", "installing", "assistant_offline", "owners_unknown" -> hubMessage(error)?.let(UiText::Plain) ?: failureReasonText(error)
    "tls_required" -> UiText.Res(R.string.machines_onb_err_tls_required)
    "mesh_member" -> UiText.Res(R.string.machines_onb_err_mesh_member)
    "too_many_pending" -> UiText.Res(R.string.machines_onb_err_too_many_pending)
    "forbidden" -> UiText.Res(R.string.machines_onb_err_forbidden)
    "invalid_name" -> UiText.Res(R.string.machines_onb_err_invalid_name)
    else -> failureReasonText(error)
}

/** The line that pairs an installed persona-link; the hub's own when it gave one. */
internal fun pairCommand(code: ComputerCode): String = code.command?.takeIf { it.isNotBlank() } ?: "persona-link pair ${quoted(code.pairUrl)}"

/** What PowerShell runs in the folder with persona-link.exe and personad.exe (docs/operations/windows.md). */
internal fun windowsCommand(pairUrl: String): String = ".\\persona-link.exe install --personad .\\personad.exe --pair ${quoted(pairUrl)}"

// A pairing link in single quotes for sh and PowerShell; it has none of its own (it is URL-encoded).
private fun quoted(s: String) = "'${s.replace("'", "%27")}'"

/** How to build and install Persona Link on a Mac until there is a download (docs/assistant/onboarding.md decision 1). */
internal const val PersonaLinkInstallDoc = "https://github.com/nwverse/persona/blob/main/docs/operations/persona-link-install.md"

/** How to install persona-link on a headless Linux when the hub has no install script (docs/operations/link.md). */
internal const val LinkLinuxDoc = "https://github.com/nwverse/persona/blob/main/docs/operations/link.md#linux-headless"

/** A computer's code lasts ten minutes, like the app's (docs/protocol/protocol.md "Computer pairing codes"). */
internal const val ComputerCodeLifeMs = 10 * 60_000L

/**
 * When a code just received runs out by this phone's clock: the hub's time, unless it is unreadable (0) or the two
 * clocks disagree so much that it reads as already gone or as longer than a code lasts; then ten minutes from now.
 */
internal fun localExpiry(expiresAt: Long, now: Long): Long =
    if (expiresAt <= now || expiresAt - now > ComputerCodeLifeMs) now + ComputerCodeLifeMs else expiresAt

/** The hub caps a computer's name at 40 characters (docs/protocol/protocol.md "Computer pairing codes"). */
internal const val ComputerNameMax = 40

internal class AddComputerModel(
    private val api: ComputerPairing,
    private val listed: StateFlow<List<Machine>>,
    confirm: String? = null,
    private val clock: () -> Long = System::currentTimeMillis,
) {
    private val mutable = MutableStateFlow(AddComputerState(now = clock()))
    val state = mutable.asStateFlow()
    // Only the latest code request may land.
    private var seq = 0

    init {
        if (confirm != null) {
            val m = pendingToConfirm(listed.value, confirm.takeIf { it.isNotBlank() })
            mutable.update { it.copy(confirming = m?.id ?: confirm.takeIf { c -> c.isNotBlank() }, confirmOnly = true, os = ComputerOs.of(m?.os ?: m?.system?.os) ?: ComputerOs.Mac) }
        }
    }

    fun setOs(os: ComputerOs) = mutable.update { it.copy(os = os) }
    fun setName(name: String) = mutable.update { it.copy(name = name.take(ComputerNameMax), error = null) }

    /**
     * "Next": the hub adds the computer and gives its code. [assistant] is the current one: the computer is its from the
     * start; none while it is offline.
     */
    suspend fun add(assistant: CurrentAssistant? = null) {
        val s = state.value
        if (s.busy || s.name.isBlank() || assistant?.offlineForAdding() != null) return
        // The roster's id for it (the default's real one too); before the roster is read, a pick of another one this
        // device kept for the hub (only a hub with several assistants has one); none from a hub without a roster.
        val manager = assistant?.let { a -> if (a.roster != null) a.id else a.choice.manager ?: a.choice.defaultId.takeIf { it != ManagerRoster.ASSISTANT_MANAGER } }
        make(asked = manager, roster = assistant?.roster) { api.add(s.name.trim(), s.os, manager) }
    }

    /** "Remove" on a computer in the way: the hub removes it, and "Next" tries again. */
    suspend fun remove(machineId: String) {
        if (state.value.removing != null) return
        mutable.update { it.copy(removing = machineId) }
        try {
            api.reject(machineId)
            mutable.update { it.copy(removing = null, tooMany = false, error = null) }
        } catch (cancelled: CancellationException) { mutable.update { it.copy(removing = null) }; throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(removing = null, tooMany = false, error = failureReasonText(error)) } }
    }

    /** A new code for the computer in hand (it never got the link, or the link expired); its old pairing is revoked. */
    suspend fun again() {
        val id = state.value.machineId ?: return
        if (state.value.busy) return
        make { api.again(id) }
    }

    // A new code for the same computer ([again]) keeps what the first one was asked for.
    private suspend fun make(asked: String? = state.value.asked, roster: ManagerRoster? = state.value.roster, call: suspend () -> ComputerCode) {
        val mine = ++seq
        mutable.update { it.copy(busy = true, error = null, tooMany = false) }
        try {
            val code = call()
            val now = clock()
            if (mine == seq) mutable.update { it.copy(code = code.copy(expiresAt = localExpiry(code.expiresAt, now)), busy = false, polled = null, seen = false, joined = false, outcome = null, now = now,
                asked = asked, roster = roster) }
        } catch (cancelled: CancellationException) { if (mine == seq) mutable.update { it.copy(busy = false) }; throw cancelled }
        catch (error: Exception) { if (mine == seq) mutable.update { it.copy(busy = false, error = addErrorText(error), tooMany = hubCodedFailure(error)?.code == "too_many_pending") } }
    }

    /** "It's mine" / "No, remove" (and "Remove" for one that never came). */
    suspend fun answer(confirm: Boolean) {
        val id = state.value.machineId ?: return
        if (state.value.answering != null) return
        val what = if (confirm) AddOutcome.Confirmed else AddOutcome.Rejected
        mutable.update { it.copy(answering = what, error = null) }
        try {
            if (confirm) api.confirm(id) else api.reject(id)
            mutable.update { it.copy(answering = null, outcome = what) }
        } catch (cancelled: CancellationException) { mutable.update { it.copy(answering = null) }; throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(answering = null, error = failureReasonText(error)) } }
    }

    fun tick() = mutable.update { s ->
        // "Confirm computer" with no computer named: the list may have had none yet when the page opened.
        val found = if (s.confirmOnly && s.confirming == null && s.code == null) pendingToConfirm(listed.value, null) else null
        val t = if (found == null) s else s.copy(confirming = found.id, os = ComputerOs.of(found.os ?: found.system?.os) ?: s.os)
        val m = t.machine(listed.value)
        t.copy(now = clock(), seen = t.seen || m != null, joined = t.joined || (t.code != null && m?.online == true))
    }

    /** Reads the machine list once (the hub of the computer in hand). */
    suspend fun poll() {
        val id = state.value.machineId ?: return
        val list = try { api.machines(id) } catch (cancelled: CancellationException) { throw cancelled } catch (_: Exception) { return }
        mutable.update { s -> if (s.machineId != id) s else s.copy(polled = list, seen = s.seen || list.any { it.id == id },
            joined = s.joined || (s.code != null && list.any { it.id == id && it.online })) }
    }

    /**
     * The countdown each second and the machine list every three, while a code waits for its computer and while it
     * asks whether it is yours (rejected or removed elsewhere, the next poll no longer has it: it is gone), and while one
     * that joined is offline (awake again, it is asked about again).
     */
    suspend fun run() {
        var n = 0
        while (true) {
            delay(1_000)
            tick()
            val s = state.value
            val waiting = s.code != null && s.outcome == null && s.stage(listed.value).let { it == AddStage.Waiting || it == AddStage.Expired || it == AddStage.Joined || it == AddStage.Offline }
            if (waiting && ++n % 3 == 0) poll()
        }
    }
}
