// Package procs caps GOMAXPROCS for Persona's own long-running processes: the hub's edge and slot, the built-in
// manager and the node.
//
// All of them are I/O-bound, yet on a machine with many cores Go keeps a P per core in each: per-P caches, more threads
// (each with its own g0 and signal stacks) and the per-P structures the runtime allocates at start. On a 64-core host
// the hub tree (edge, slot, manager and its node) holds about 10 MB more anonymous memory and 50 more threads than
// with 4, for no measurable speed (#3533).
//
// AtStart caps a process in-process with runtime.GOMAXPROCS, so the agents, extensions and tools it starts keep their
// own default, and only when the runtime's default is above the cap: an explicit value stops the runtime from following
// a container's CPU limit, so a host or container with Max CPUs or fewer keeps the runtime's value and its updates.
// A GOMAXPROCS variable the user sets always wins, and passes on unchanged.
//
// The slot and the manager are the hub's own children (edge -> slot -> manager). ChildEnv hands them the cap in their
// environment (GOMAXPROCS and EnvCap), so the runtime starts them with Max Ps instead of allocating one per core and
// giving them back; their AtStart takes both variables out again, so nothing they start sees them.
package procs

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

const (
	// Max is the most Ps a capped process keeps.
	Max = 4
	// EnvCap marks a GOMAXPROCS that ChildEnv set, not the user: the child's AtStart removes both and passes the cap
	// on to its own children.
	EnvCap      = "PERSONA_GOMAXPROCS_CAP"
	envMaxProcs = "GOMAXPROCS"
)

// capped is the cap this process runs under and hands its own children (ChildEnv); 0 when it runs uncapped. AtStart
// sets it at the process's entry, before it starts anything.
var capped int

// AtStart is the first thing a capped process does after taking over what its parent left (osproc.ReapInherited,
// hub claimInherited), before it starts any child. With the marker ChildEnv sets, the runtime has already read the
// cap: both variables are removed and the cap is kept for this process's own ChildEnv. Without it, the process caps
// itself at Max unless GOMAXPROCS is set or the runtime's default is at most Max. A marker that does not match
// GOMAXPROCS means someone else set GOMAXPROCS: the marker goes and GOMAXPROCS is the user's.
func AtStart() {
	if mark, ok := os.LookupEnv(EnvCap); ok {
		_ = os.Unsetenv(EnvCap)
		if n, err := strconv.Atoi(mark); err == nil && n > 0 && os.Getenv(envMaxProcs) == mark {
			_ = os.Unsetenv(envMaxProcs)
			capped = n
			return
		}
	}
	capTo(Max)
}

// capTo lowers GOMAXPROCS to n when the GOMAXPROCS variable is not set and the runtime's default is above n.
func capTo(n int) {
	if os.Getenv(envMaxProcs) != "" {
		return
	}
	if runtime.GOMAXPROCS(0) > n {
		runtime.GOMAXPROCS(n)
		capped = n
	}
}

// ChildEnv is env for one of the hub's own children (the edge's slot, the slot's manager): with this process's cap
// added when it runs capped and env sets neither variable. Anything else the process starts gets its environment as
// it is, which no longer holds the cap.
func ChildEnv(env []string) []string {
	if capped == 0 {
		return env
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, envMaxProcs+"=") || strings.HasPrefix(kv, EnvCap+"=") {
			return env
		}
	}
	n := strconv.Itoa(capped)
	return append(env, envMaxProcs+"="+n, EnvCap+"="+n)
}
