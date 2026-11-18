package proto

const MethodRoutineCheck = "routine.check"

const (
	RoutineCheckDefaultTimeout = 30
	RoutineCheckMaxTimeout     = 60
	RoutineCheckMaxCommand     = 4096
)

type RoutineCheckParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout_s"`
}

type RoutineCheckResult struct {
	ExitCode int    `json:"exit_code"`
	Reason   string `json:"reason,omitempty"`
}
