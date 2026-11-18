package persona.feature.machines

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.Machine
import persona.core.contracts.MachineMetrics
import persona.core.contracts.MetricSample
import persona.core.contracts.Session

class MachineConcernTest {
    private val now = 1_000L

    private fun session(machine: String, id: String, state: String): Session {
        val status = when (state) {
            "needs_you" -> "blocked"
            "unread" -> "done"
            else -> state
        }
        return Session(machine, id, state = state, status = status, title = id)
    }

    private fun samplesAtLevel(critical: Boolean): List<MetricSample> = (0..100)
        .map { MetricSample(now, cpu = it.toDouble()) }
        .filter { sample ->
            val current = alerts(sample, cores = null)
            if (critical) current.any { it.critical } else current.isNotEmpty() && current.none { it.critical }
        }

    private fun metric(id: String, sample: MetricSample? = null, online: Boolean = true) =
        MachineMetrics(id, online = online, latest = sample)

    @Test
    fun allConcernsHaveTheExpectedPriority() {
        val critical = samplesAtLevel(critical = true).first()
        val warning = samplesAtLevel(critical = false).first()
        val machines = listOf(
            Machine("normal", online = true),
            Machine("mismatch", online = true, nodeMismatch = true),
            Machine("needs", online = true),
            Machine("warning", online = true),
            Machine("critical", online = true),
            Machine("offline", online = false),
            Machine("pending", online = false, trust = "pending"),
        )
        val metrics = machines.associate { machine ->
            machine.id to when (machine.id) {
                "critical" -> metric(machine.id, critical)
                "warning" -> metric(machine.id, warning)
                "offline", "pending" -> metric(machine.id, online = false)
                else -> metric(machine.id)
            }
        }
        val sessions = listOf(session("needs", "waiting", "needs_you"), session("critical", "also-waiting", "needs_you"))

        val ordered = machineListEntries(machineListIds(machines, metrics), machines, metrics, sessions, now)

        assertEquals(listOf("pending", "offline", "critical", "warning", "needs", "mismatch", "normal"), ordered.map { it.id })
        assertEquals(MachineConcern.Pending, ordered[0].concern)
        assertEquals(MachineConcern.Critical, ordered[2].concern)
    }

    @Test
    fun equalRanksAndUnchangedAlertLevelsKeepTheExactIdSequence() {
        val firstWarning = samplesAtLevel(critical = false).first()
        val lastWarning = samplesAtLevel(critical = false).last()
        val machines = listOf(Machine("a", online = true), Machine("b", online = true), Machine("c", online = true))
        val sessions = listOf(session("b", "working", "working"), session("b", "unread", "unread"))
        val before = mapOf("a" to metric("a", firstWarning), "b" to metric("b"), "c" to metric("c", firstWarning))
        val after = mapOf("a" to metric("a", lastWarning), "b" to metric("b"), "c" to metric("c", lastWarning))
        val ids = machineListIds(machines, before)

        val first = machineListEntries(ids, machines, before, sessions, now)
        val second = machineListEntries(ids, machines, after, sessions, now)

        assertEquals(listOf("a", "c", "b"), first.map { it.id })
        assertEquals(first.map { it.id }, second.map { it.id })
        assertEquals(first.map { it.id }, listOf("a", "c", "b"))
        assertEquals(first.map { it.tally }, second.map { it.tally })
        assertEquals(machineGridRows(first.map { it.id }, 2).map(::machineGridRowKey),
            machineGridRows(second.map { it.id }, 2).map(::machineGridRowKey))
    }

    @Test
    fun onlyAVisibleConcernChangeMovesAnOrdinaryComputer() {
        val machine = Machine("machine", online = true, nodeVersion = "one-version")
        val metric = metric("machine")
        val workingAndUnread = listOf(
            session("machine", "working", "working"),
            session("machine", "unread", "unread"),
            session("machine", "background-waiting", "working").copy(backgroundWaiting = true),
        )
        val needsYou = workingAndUnread + session("machine", "waiting", "needs_you")

        fun concern(
            currentMachine: Machine = machine,
            currentMetric: MachineMetrics = metric,
            sessions: List<Session> = workingAndUnread,
        ) = machineListEntries(listOf("machine"), listOf(currentMachine), mapOf("machine" to currentMetric), sessions, now).single().concern

        assertEquals(MachineConcern.None, concern())
        assertEquals(MachineConcern.NeedsYou, concern(sessions = needsYou))
        assertEquals(MachineConcern.None, concern())
        assertEquals(MachineConcern.Mismatch, concern(currentMachine = machine.copy(nodeMismatch = true)))
        assertEquals(MachineConcern.Mismatch, concern(currentMachine = machine.copy(deploying = true)))
        assertEquals(MachineConcern.None, concern(currentMachine = machine.copy(nodeVersion = "another-version")))
        assertEquals(MachineConcern.Offline, concern(
            currentMachine = machine.copy(online = false),
            currentMetric = metric.copy(online = false),
        ))
    }

    @Test
    fun alertSeverityComesFromTheExistingAlertsProjection() {
        val machine = Machine("machine", online = true)
        val warning = samplesAtLevel(critical = false).first()
        val critical = samplesAtLevel(critical = true).first()

        fun concern(sample: MetricSample) = machineListEntries(
            listOf("machine"), listOf(machine), mapOf("machine" to metric("machine", sample)), emptyList(), now,
        ).single().concern

        assertEquals(MachineConcern.Warning, concern(warning))
        assertEquals(MachineConcern.Critical, concern(critical))
        assertEquals(MachineConcern.Warning, concern(critical.copy(cpu = warning.cpu)))
    }

    @Test
    fun managerScopedAndMetricsOnlyInputsAreNotDropped() {
        val machines = listOf(
            Machine("one", online = true, manager = "assistant-one"),
            Machine("two", online = true, manager = "assistant-two"),
        )
        val metrics = mapOf("one" to metric("one"), "two" to metric("two"), "metrics-only" to metric("metrics-only"))
        val ids = machineListIds(machines, metrics)

        val entries = machineListEntries(ids, machines, metrics, emptyList(), now)

        assertEquals(listOf("one", "two", "metrics-only"), ids)
        assertEquals(ids, entries.map { it.id })
        assertNull(entries.last().machine)
        assertEquals(MachineConcern.None, entries.last().concern)
    }
}
