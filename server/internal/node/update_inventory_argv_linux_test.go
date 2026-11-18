package node

import (
	"os"
	"slices"
	"testing"
)

func TestUpdateProcessKernelArgv(t *testing.T) {
	args, err := updateProcessArgv(os.Getpid())
	if err != nil || !slices.Equal(args, os.Args) {
		t.Fatalf("kernel arguments differ from the live process: %v", err)
	}
}

// A process's executable is the kernel's, whatever the process calls itself (the Paseo bridge's Node.js, #8134).
func TestProcessExecutableIsTheKernels(t *testing.T) {
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got := processExecutable(os.Getpid()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := processExecutable(-1); got != "" {
		t.Fatalf("no such process: %q", got)
	}
}
