package proto

// A computer's diagnosis and cache cleanup (#8037, docs/server/machines.md "Diagnosis and cleanup"): the node answers a
// fixed, bounded question about what uses its memory, CPU and disk, and empties caches from its own catalog. Nothing in
// these types names a caller-chosen path or command: sections and ids are closed lists, and every path is the node's.

// The node methods (capability nodeproto.MachineDiagnose).
const (
	MethodMachineDiagnose = "machine.diagnose" // DiagnoseParams → Diagnosis (without MachineID)
	MethodMachineCleanup  = "machine.cleanup"  // CleanupParams → CleanupResult
)

// The sections of a diagnosis; an empty request asks for all of them.
const (
	DiagnoseProcesses = "processes"
	DiagnoseDisk      = "disk"
	DiagnoseTools     = "tools"
)

// DiagnoseSections are every section, in the order they are documented.
var DiagnoseSections = []string{DiagnoseProcesses, DiagnoseDisk, DiagnoseTools}

// CacheIDs are the rebuildable caches a node knows (each with its own path per OS; one a system does not have is never
// listed). pnpm's store is left out on purpose: its files are hard-linked into projects, so emptying it frees little
// and breaks nothing only by luck.
var CacheIDs = []string{"go-build", "go-mod", "npm-cache", "pip-cache", "uv-cache", "yarn-cache", "homebrew-cache",
	"xcode-derived-data", "gradle-cache", "cargo-registry"}

// ToolIDs are the tools a node looks for, each found by its own command on the node's PATH.
var ToolIDs = []string{"xcode", "docker", "podman", "go", "node", "python", "rust", "java", "swift", "git", "gh", "brew",
	"bun", "pnpm", "uv", "tmux", "claude", "codex", "opencode", "gemini"}

// Process group kinds.
const (
	ProcessKindAgent   = "agent"   // an Agent's CLI (Claude Code, Codex, …)
	ProcessKindEngine  = "engine"  // an engine or terminal multiplexer Agents run in
	ProcessKindPersona = "persona" // Persona's own programs
	ProcessKindOther   = "other"
)

// Cleanup item outcomes.
const (
	CleanupCleaned = "cleaned"
	CleanupInUse   = "in_use"  // a process of the owning toolchain runs: nothing was removed
	CleanupMissing = "missing" // the cache is not on this computer, or is empty
	CleanupFailed  = "failed"  // with Error
)

// DiagnoseParams is machine.diagnose's params.
type DiagnoseParams struct {
	Sections []string `json:"sections,omitempty"`
}

// Diagnosis is what a computer reports about itself. A section is present when it was asked for and this computer can
// answer it.
type Diagnosis struct {
	MachineID string         `json:"machine_id,omitempty"` // set by the manager
	At        int64          `json:"at"`                   // Unix seconds
	Processes *ProcessReport `json:"processes,omitempty"`
	Disk      *DiskReport    `json:"disk,omitempty"`
	// Tools are the detected tools only; a pointer so that "none found" ([]) differs from "not reported".
	Tools *[]DiagnosedTool `json:"tools,omitempty"`
}

// ProcessReport is the largest process groups. Names only: never arguments, command lines or environment, which may
// hold secrets.
type ProcessReport struct {
	MemTotal int64          `json:"mem_total"` // bytes
	Count    int            `json:"count"`     // processes seen
	Groups   []ProcessGroup `json:"groups"`    // top 15 by RSS and top 5 by CPU, sorted by RSS, largest first
}

// ProcessGroup is the processes of one executable name.
type ProcessGroup struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"` // ProcessKind*
	Count int    `json:"count"`
	RSS   int64  `json:"rss"` // bytes, summed over the group
	// CPU is the percent of one core, summed over the group; nil when it could not be measured.
	CPU *float64 `json:"cpu,omitempty"`
}

// DiskReport is the volumes, the largest folders under the home directory and the caches found.
type DiskReport struct {
	Volumes []DiskVolume `json:"volumes"`
	Folders []DiskFolder `json:"folders"` // direct children of home, largest first, at most 12
	Partial bool         `json:"partial"` // the bounded scan was cut short: sizes are lower bounds
	Caches  []DiskCache  `json:"caches"`  // only caches that exist and hold something
}

// DiskVolume is one local filesystem, like df: total = used + what an ordinary user can still write.
type DiskVolume struct {
	Mount string `json:"mount"`
	Used  int64  `json:"used"`
	Total int64  `json:"total"`
}

// DiskFolder is a folder and the bytes it holds on the home directory's filesystem.
type DiskFolder struct {
	Path  string `json:"path"` // "~/…"
	Bytes int64  `json:"bytes"`
}

// DiskCache is one cache of the catalog (CacheIDs).
type DiskCache struct {
	ID    string `json:"id"`
	Path  string `json:"path"` // "~/…"
	Bytes int64  `json:"bytes"`
	InUse bool   `json:"in_use"` // a process of its toolchain runs (or the processes could not be read)
}

// DiagnosedTool is one tool of ToolIDs found on the computer.
type DiagnosedTool struct {
	ID      string `json:"id"`
	Version string `json:"version"` // "" when it could not be read
}

// CleanupParams is machine.cleanup's params: ids of CacheIDs, never paths.
type CleanupParams struct {
	IDs []string `json:"ids"`
}

// CleanupResult is what a cleanup did, measured on the home directory's volume afterwards.
type CleanupResult struct {
	FreedBytes      int64         `json:"freed_bytes"` // available after - before, never below 0
	AvailableBefore int64         `json:"available_before"`
	AvailableAfter  int64         `json:"available_after"`
	Items           []CleanupItem `json:"items"`
}

// CleanupItem is one cache's outcome.
type CleanupItem struct {
	ID     string `json:"id"`
	Bytes  int64  `json:"bytes"`  // what it held before
	Status string `json:"status"` // Cleanup*
	Error  string `json:"error,omitempty"`
}
